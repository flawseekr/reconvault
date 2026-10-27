package artifact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultUserAgent is sent when the caller does not provide a User-Agent.
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36"

// Input limits for outbound requests.
const (
	MaxURLLength = 8192
	// MaxHostnameLength is the DNS limit (RFC 1035); it also keeps hostname
	// well below the B-tree index entry size limit.
	MaxHostnameLength = 253
	MaxHeaderCount    = 100
	maxHeaderName     = 256
	maxHeaderValue    = 16 << 10
)

// Result-size limits shared by the list and search endpoints.
const (
	DefaultLimit = 30
	MaxLimit     = 100

	// The domain listing is not limited by the specification; a generous
	// default and a hard cap keep responses bounded.
	DefaultDomainLimit = 100
	MaxDomainLimit     = 1000
)

// persistTimeout bounds the database write that records an artifact. The
// write is detached from the caller's context so that an attempted request
// is recorded even if the client disconnects mid-flight.
const persistTimeout = 10 * time.Second

// allowedMethods is the set of HTTP methods the service will send.
// CONNECT is excluded because it establishes tunnels rather than requests.
var allowedMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodPatch:   true,
	http.MethodDelete:  true,
	http.MethodOptions: true,
	http.MethodTrace:   true,
}

// ExecuteInput is the client-supplied description of a request to send.
type ExecuteInput struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    *string
}

// OutboundRequest is a validated request ready to be sent by a Fetcher.
type OutboundRequest struct {
	Method  string
	URL     *url.URL
	Headers http.Header
	Body    *string
}

// Response is what a Fetcher returns for a completed exchange.
type Response struct {
	StatusCode  int
	ContentType string
	// Head is the raw response head: status line, header lines and the
	// terminating blank line, all CRLF-delimited.
	Head []byte
	// Body holds at most the configured maximum number of bytes.
	Body []byte
	// SizeBytes is the full size of the response body: the number of bytes
	// read, or the declared Content-Length when the body was truncated.
	SizeBytes int64
	Truncated bool
}

// Fetcher performs outbound HTTP requests.
type Fetcher interface {
	Do(ctx context.Context, req OutboundRequest) (*Response, error)
}

// Repository persists and queries artifacts.
type Repository interface {
	Create(ctx context.Context, a *Artifact) error
	Get(ctx context.Context, id string) (*Artifact, error)
	// List returns up to p.Limit summaries, honoring p.After.
	List(ctx context.Context, p ListParams) ([]Summary, error)
	Search(ctx context.Context, p SearchParams) ([]SearchResult, error)
	Domains(ctx context.Context, p DomainParams) ([]Domain, error)
	// Delete removes the artifact or returns ErrNotFound.
	Delete(ctx context.Context, id string) error
}

// Service implements the artifact use cases.
type Service struct {
	repo    Repository
	fetcher Fetcher
	now     func() time.Time
}

// NewService constructs a Service.
func NewService(repo Repository, fetcher Fetcher) *Service {
	return &Service{repo: repo, fetcher: fetcher, now: time.Now}
}

// ExecuteResult is the outcome of a successfully executed request.
type ExecuteResult struct {
	Artifact *Artifact
	// Truncated reports that the response body exceeded the storage limit.
	Truncated bool
}

// Execute validates the input, sends the request, and records the exchange.
//
// Validation failures return a *ValidationError and blocked destinations
// return ErrDestinationNotAllowed; neither is recorded. A transport failure
// after the request was attempted is recorded with empty response fields
// and reported as an *UpstreamError.
func (s *Service) Execute(ctx context.Context, in ExecuteInput) (*ExecuteResult, error) {
	req, err := validateExecuteInput(in)
	if err != nil {
		return nil, err
	}
	if req.Headers.Get("User-Agent") == "" {
		req.Headers.Set("User-Agent", DefaultUserAgent)
	}

	headersJSON, err := json.Marshal(flattenHeaders(req.Headers))
	if err != nil {
		return nil, fmt.Errorf("encode request headers: %w", err)
	}

	a := &Artifact{
		HTTPMethod:     req.Method,
		Hostname:       strings.ToLower(req.URL.Hostname()),
		EndpointPath:   endpointPath(req.URL),
		FullURL:        req.URL.String(),
		RequestedAt:    s.now().UTC().Truncate(time.Microsecond), // PostgreSQL precision
		RequestHeaders: headersJSON,
	}
	if req.Body != nil {
		a.RequestBody = ptr(sanitizeText([]byte(*req.Body)))
	}

	resp, fetchErr := s.fetcher.Do(ctx, req)
	if errors.Is(fetchErr, ErrDestinationNotAllowed) {
		return nil, fetchErr
	}
	if fetchErr == nil {
		a.ResponseStatusCode = ptr(resp.StatusCode)
		a.ResponseBodySizeBytes = ptr(resp.SizeBytes)
		raw := make([]byte, 0, len(resp.Head)+len(resp.Body))
		a.Response = ptr(sanitizeText(append(append(raw, resp.Head...), resp.Body...)))
		if resp.ContentType != "" {
			a.ResponseContentType = ptr(sanitizeText([]byte(resp.ContentType)))
		}
	}

	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer cancel()
	if err := s.repo.Create(persistCtx, a); err != nil {
		return nil, fmt.Errorf("store artifact: %w", err)
	}

	if fetchErr != nil {
		return nil, &UpstreamError{ArtifactID: a.ID, Timeout: isTimeout(fetchErr), Err: fetchErr}
	}
	return &ExecuteResult{Artifact: a, Truncated: resp.Truncated}, nil
}

// Get returns a single artifact by ID or ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (*Artifact, error) {
	if !IsUUID(id) {
		return nil, invalid("id", "must be a valid UUID")
	}
	return s.repo.Get(ctx, strings.ToLower(id))
}

// Delete permanently removes an artifact. It returns ErrNotFound when no
// artifact has the given ID.
func (s *Service) Delete(ctx context.Context, id string) error {
	if !IsUUID(id) {
		return invalid("id", "must be a valid UUID")
	}
	return s.repo.Delete(ctx, strings.ToLower(id))
}

// List returns one page of artifact summaries and the cursor of the next
// page, if any.
func (s *Service) List(ctx context.Context, p ListParams) (*ListPage, error) {
	if c := p.After; c != nil {
		if err := c.Validate(); err != nil {
			return nil, err
		}
		if c.SortBy != p.SortBy || c.SortOrder != p.SortOrder || c.Hostname != p.Hostname {
			return nil, invalid("cursor", "does not match sort_by, sort_order and hostname")
		}
	}

	// Fetch one extra row to learn whether another page exists.
	q := p
	q.Limit = p.Limit + 1
	items, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, err
	}
	page := &ListPage{Items: items}
	if len(items) > p.Limit {
		page.Items = items[:p.Limit]
		last := page.Items[p.Limit-1]
		page.Next = &ListCursor{
			SortBy:    p.SortBy,
			SortOrder: p.SortOrder,
			Hostname:  p.Hostname,
			Value:     last.SortValue(p.SortBy),
			ID:        last.ID,
		}
	}
	return page, nil
}

// Search returns artifacts whose fields contain the query (case-insensitive).
func (s *Service) Search(ctx context.Context, p SearchParams) ([]SearchResult, error) {
	return s.repo.Search(ctx, p)
}

// Domains returns per-hostname aggregates.
func (s *Service) Domains(ctx context.Context, p DomainParams) ([]Domain, error) {
	return s.repo.Domains(ctx, p)
}

func validateExecuteInput(in ExecuteInput) (OutboundRequest, error) {
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		return OutboundRequest{}, invalid("method", "is required")
	}
	if !allowedMethods[method] {
		return OutboundRequest{}, invalid("method", "must be one of GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS, TRACE")
	}

	u, err := validateURL(in.URL)
	if err != nil {
		return OutboundRequest{}, err
	}

	headers, err := validateHeaders(in.Headers)
	if err != nil {
		return OutboundRequest{}, err
	}

	return OutboundRequest{Method: method, URL: u, Headers: headers, Body: in.Body}, nil
}

func validateURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, invalid("url", "is required")
	}
	if len(raw) > MaxURLLength {
		return nil, invalid("url", fmt.Sprintf("must be at most %d characters", MaxURLLength))
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, invalid("url", "is not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, invalid("url", "scheme must be http or https")
	}
	if u.Hostname() == "" {
		return nil, invalid("url", "must include a host")
	}
	if len(u.Hostname()) > MaxHostnameLength {
		return nil, invalid("url", fmt.Sprintf("host must be at most %d characters", MaxHostnameLength))
	}
	if !utf8.ValidString(u.String()) {
		return nil, invalid("url", "must be valid UTF-8")
	}
	return u, nil
}

func validateHeaders(in map[string]string) (http.Header, error) {
	if len(in) > MaxHeaderCount {
		return nil, invalid("headers", fmt.Sprintf("must contain at most %d entries", MaxHeaderCount))
	}
	h := make(http.Header, len(in)+1)
	for name, value := range in {
		if !validHeaderName(name) {
			return nil, invalid("headers", fmt.Sprintf("invalid header name %q", name))
		}
		if !validHeaderValue(value) {
			return nil, invalid("headers", fmt.Sprintf("invalid value for header %q", name))
		}
		canonical := http.CanonicalHeaderKey(name)
		if _, dup := h[canonical]; dup {
			return nil, invalid("headers", fmt.Sprintf("duplicate header %q", name))
		}
		h[canonical] = []string{value}
	}
	return h, nil
}

// validHeaderName reports whether name is an RFC 9110 field-name (token).
func validHeaderName(name string) bool {
	if name == "" || len(name) > maxHeaderName {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// validHeaderValue rejects control characters (except HTAB), which would
// otherwise allow header injection, and invalid UTF-8.
func validHeaderValue(v string) bool {
	if len(v) > maxHeaderValue || !utf8.ValidString(v) {
		return false
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; (c < 0x20 && c != '\t') || c == 0x7f {
			return false
		}
	}
	return true
}

// flattenHeaders converts headers to the single-valued object stored in
// request_headers.
func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = strings.Join(v, ", ")
	}
	return out
}

func endpointPath(u *url.URL) string {
	if p := u.EscapedPath(); p != "" {
		return p
	}
	return "/"
}

// sanitizeText makes arbitrary bytes storable in a PostgreSQL TEXT column,
// which must be valid UTF-8 and cannot contain NUL. Invalid sequences and
// NUL bytes are replaced with U+FFFD.
func sanitizeText(b []byte) string {
	if utf8.Valid(b) && bytes.IndexByte(b, 0) < 0 {
		return string(b) // common case: a single copy of up to MaxBodyBytes
	}
	s := strings.ToValidUTF8(string(b), "\uFFFD")
	return strings.ReplaceAll(s, "\x00", "\uFFFD")
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// IsUUID reports whether s is a UUID in canonical 8-4-4-4-12 hex form.
func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

func ptr[T any](v T) *T { return &v }
