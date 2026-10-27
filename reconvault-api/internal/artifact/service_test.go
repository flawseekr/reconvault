package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

type fakeFetcher struct {
	got  OutboundRequest
	resp *Response
	err  error
}

func (f *fakeFetcher) Do(_ context.Context, req OutboundRequest) (*Response, error) {
	f.got = req
	return f.resp, f.err
}

type fakeRepo struct {
	listP     ListParams
	listItems []Summary
	deleteID  string
	deleteErr error
	created   []*Artifact
	createErr error
	ctxErr    error // ctx.Err() observed in Create
	getID     string
}

func (r *fakeRepo) Create(ctx context.Context, a *Artifact) error {
	r.ctxErr = ctx.Err()
	if r.createErr != nil {
		return r.createErr
	}
	a.ID = "00000000-0000-0000-0000-000000000001"
	r.created = append(r.created, a)
	return nil
}
func (r *fakeRepo) Get(_ context.Context, id string) (*Artifact, error) {
	r.getID = id
	return nil, ErrNotFound
}
func (r *fakeRepo) List(_ context.Context, p ListParams) ([]Summary, error) {
	r.listP = p
	if len(r.listItems) > p.Limit {
		return r.listItems[:p.Limit], nil
	}
	return r.listItems, nil
}
func (r *fakeRepo) Delete(_ context.Context, id string) error {
	r.deleteID = id
	return r.deleteErr
}
func (r *fakeRepo) Search(context.Context, SearchParams) ([]SearchResult, error) { return nil, nil }
func (r *fakeRepo) Domains(context.Context, DomainParams) ([]Domain, error)      { return nil, nil }

var fixedNow = time.Date(2026, 10, 27, 12, 30, 0, 123456789, time.FixedZone("X", 7*3600))

func newTestService(repo Repository, f Fetcher) *Service {
	s := NewService(repo, f)
	s.now = func() time.Time { return fixedNow }
	return s
}

const okHead = "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nX-Secret: s3\r\n\r\n"

func okResponse() *Response {
	return &Response{StatusCode: 200, ContentType: "application/json", Head: []byte(okHead), Body: []byte(`{"success":true}`), SizeBytes: 16}
}

func TestExecuteSuccess(t *testing.T) {
	repo := &fakeRepo{}
	f := &fakeFetcher{resp: okResponse()}
	body := `{"id":123,"name":"test"}`
	res, err := newTestService(repo, f).Execute(context.Background(), ExecuteInput{
		Method:  "post",
		URL:     "https://API.Example.com:8443/v1/resource?x=1",
		Headers: map[string]string{"content-type": "application/json"},
		Body:    &body,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	a := res.Artifact
	if a.ID == "" || len(repo.created) != 1 {
		t.Fatalf("artifact not stored: %+v", repo.created)
	}
	if a.HTTPMethod != "POST" || f.got.Method != "POST" {
		t.Errorf("method not normalized: %q / %q", a.HTTPMethod, f.got.Method)
	}
	if a.Hostname != "api.example.com" {
		t.Errorf("hostname = %q", a.Hostname)
	}
	if a.EndpointPath != "/v1/resource" {
		t.Errorf("endpoint_path = %q", a.EndpointPath)
	}
	if a.FullURL != "https://API.Example.com:8443/v1/resource?x=1" {
		t.Errorf("full_url = %q", a.FullURL)
	}
	if want := fixedNow.UTC().Truncate(time.Microsecond); !a.RequestedAt.Equal(want) || a.RequestedAt.Location() != time.UTC {
		t.Errorf("requested_at = %v, want %v UTC", a.RequestedAt, want)
	}
	if *a.ResponseStatusCode != 200 || *a.ResponseContentType != "application/json" ||
		*a.ResponseBodySizeBytes != 16 || *a.Response != okHead+`{"success":true}` || *a.RequestBody != body {
		t.Errorf("response fields wrong: %+v", a)
	}
	var headers map[string]string
	if err := json.Unmarshal(a.RequestHeaders, &headers); err != nil {
		t.Fatal(err)
	}
	if headers["Content-Type"] != "application/json" || headers["User-Agent"] != DefaultUserAgent {
		t.Errorf("stored headers = %v", headers)
	}
	if f.got.Headers.Get("User-Agent") != DefaultUserAgent {
		t.Errorf("default User-Agent not sent")
	}
	if res.Truncated {
		t.Error("unexpected truncation")
	}
}

func TestExecuteKeepsCallerUserAgent(t *testing.T) {
	for _, name := range []string{"User-Agent", "user-agent", "USER-AGENT"} {
		f := &fakeFetcher{resp: okResponse()}
		_, err := newTestService(&fakeRepo{}, f).Execute(context.Background(), ExecuteInput{
			Method: "GET", URL: "http://example.com", Headers: map[string]string{name: "custom/1.0"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := f.got.Headers.Values("User-Agent"); len(got) != 1 || got[0] != "custom/1.0" {
			t.Errorf("%s: User-Agent = %v", name, got)
		}
	}
}

func TestExecuteEmptyUserAgentGetsDefault(t *testing.T) {
	f := &fakeFetcher{resp: okResponse()}
	_, err := newTestService(&fakeRepo{}, f).Execute(context.Background(), ExecuteInput{
		Method: "GET", URL: "http://example.com", Headers: map[string]string{"User-Agent": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.got.Headers.Get("User-Agent") != DefaultUserAgent {
		t.Errorf("User-Agent = %q", f.got.Headers.Get("User-Agent"))
	}
}

func TestExecuteDefaults(t *testing.T) {
	repo := &fakeRepo{}
	_, err := newTestService(repo, &fakeFetcher{resp: &Response{StatusCode: 204}}).Execute(
		context.Background(), ExecuteInput{Method: "GET", URL: "http://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	a := repo.created[0]
	if a.EndpointPath != "/" {
		t.Errorf("empty path should be stored as /, got %q", a.EndpointPath)
	}
	if a.RequestBody != nil {
		t.Errorf("omitted body should be NULL, got %q", *a.RequestBody)
	}
	if a.ResponseContentType != nil {
		t.Errorf("missing content type should be NULL")
	}
	if *a.Response != "" || *a.ResponseBodySizeBytes != 0 {
		t.Errorf("empty body fields wrong")
	}
}

func TestExecuteEmptyBodyIsStoredAsEmpty(t *testing.T) {
	repo := &fakeRepo{}
	empty := ""
	_, err := newTestService(repo, &fakeFetcher{resp: okResponse()}).Execute(context.Background(),
		ExecuteInput{Method: "POST", URL: "http://example.com", Body: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if b := repo.created[0].RequestBody; b == nil || *b != "" {
		t.Errorf("explicit empty body should be stored as empty string, got %v", b)
	}
}

func TestExecuteValidation(t *testing.T) {
	tests := []struct {
		name  string
		in    ExecuteInput
		field string
	}{
		{"missing method", ExecuteInput{URL: "http://example.com"}, "method"},
		{"blank method", ExecuteInput{Method: "  ", URL: "http://example.com"}, "method"},
		{"unsupported method", ExecuteInput{Method: "CONNECT", URL: "http://example.com"}, "method"},
		{"invalid method token", ExecuteInput{Method: "GE T", URL: "http://example.com"}, "method"},
		{"missing url", ExecuteInput{Method: "GET"}, "url"},
		{"relative url", ExecuteInput{Method: "GET", URL: "/path"}, "url"},
		{"ftp scheme", ExecuteInput{Method: "GET", URL: "ftp://example.com"}, "url"},
		{"file scheme", ExecuteInput{Method: "GET", URL: "file:///etc/passwd"}, "url"},
		{"no host", ExecuteInput{Method: "GET", URL: "http://:80/x"}, "url"},
		{"unparseable", ExecuteInput{Method: "GET", URL: "http://exa mple.com"}, "url"},
		{"control char", ExecuteInput{Method: "GET", URL: "http://example.com/\x7f"}, "url"},
		{"too long", ExecuteInput{Method: "GET", URL: "http://example.com/" + strings.Repeat("a", MaxURLLength)}, "url"},
		{"hostname too long", ExecuteInput{Method: "GET", URL: "http://" + strings.Repeat("a", MaxHostnameLength+1) + "/"}, "url"},
		{"bad header name", ExecuteInput{Method: "GET", URL: "http://example.com", Headers: map[string]string{"Bad Header": "x"}}, "headers"},
		{"empty header name", ExecuteInput{Method: "GET", URL: "http://example.com", Headers: map[string]string{"": "x"}}, "headers"},
		{"header CRLF injection", ExecuteInput{Method: "GET", URL: "http://example.com", Headers: map[string]string{"X-A": "a\r\nX-B: b"}}, "headers"},
		{"header NUL", ExecuteInput{Method: "GET", URL: "http://example.com", Headers: map[string]string{"X-A": "a\x00"}}, "headers"},
		{"header invalid utf8", ExecuteInput{Method: "GET", URL: "http://example.com", Headers: map[string]string{"X-A": "\xff"}}, "headers"},
		{"duplicate header", ExecuteInput{Method: "GET", URL: "http://example.com", Headers: map[string]string{"X-A": "1", "x-a": "2"}}, "headers"},
		{"too many headers", ExecuteInput{Method: "GET", URL: "http://example.com", Headers: manyHeaders(MaxHeaderCount + 1)}, "headers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			f := &fakeFetcher{resp: okResponse()}
			_, err := newTestService(repo, f).Execute(context.Background(), tt.in)
			var ve *ValidationError
			if !errors.As(err, &ve) || ve.Field != tt.field {
				t.Fatalf("want validation error on %q, got %v", tt.field, err)
			}
			if len(repo.created) != 0 || f.got.URL != nil {
				t.Fatal("invalid input must not be sent or stored")
			}
		})
	}
}

func TestExecuteAcceptsBoundaryInput(t *testing.T) {
	u := "http://example.com/"
	u += strings.Repeat("a", MaxURLLength-len(u))
	_, err := newTestService(&fakeRepo{}, &fakeFetcher{resp: okResponse()}).Execute(context.Background(), ExecuteInput{
		Method: "delete", URL: u, Headers: manyHeaders(MaxHeaderCount),
	})
	if err != nil {
		t.Fatalf("boundary input rejected: %v", err)
	}
}

func TestExecuteAcceptsMaxHostname(t *testing.T) {
	_, err := newTestService(&fakeRepo{}, &fakeFetcher{resp: okResponse()}).Execute(context.Background(), ExecuteInput{
		Method: "GET", URL: "http://" + strings.Repeat("a", MaxHostnameLength) + "/",
	})
	if err != nil {
		t.Fatalf("253-character hostname rejected: %v", err)
	}
}

func TestExecuteAllowsTabInHeaderValue(t *testing.T) {
	_, err := newTestService(&fakeRepo{}, &fakeFetcher{resp: okResponse()}).Execute(context.Background(), ExecuteInput{
		Method: "GET", URL: "http://example.com", Headers: map[string]string{"X-Tab": "a\tb", "X-Tok!#$%&'*+-.^_`|~": "ok"},
	})
	if err != nil {
		t.Fatalf("valid headers rejected: %v", err)
	}
}

func manyHeaders(n int) map[string]string {
	m := make(map[string]string, n)
	for i := range n {
		m["X-H-"+strings.Repeat("a", i+1)] = "v"
	}
	return m
}

func TestExecuteBlockedDestinationNotStored(t *testing.T) {
	repo := &fakeRepo{}
	_, err := newTestService(repo, &fakeFetcher{err: ErrDestinationNotAllowed}).Execute(
		context.Background(), ExecuteInput{Method: "GET", URL: "http://127.0.0.1"})
	if !errors.Is(err, ErrDestinationNotAllowed) {
		t.Fatalf("err = %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatal("blocked request must not be stored")
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

func TestExecuteUpstreamFailureIsStored(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantTimeout bool
	}{
		{"connection refused", errors.New("connection refused"), false},
		{"net timeout", timeoutErr{}, true},
		{"deadline", context.DeadlineExceeded, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			_, err := newTestService(repo, &fakeFetcher{err: tt.err}).Execute(
				context.Background(), ExecuteInput{Method: "GET", URL: "http://example.com/x"})
			var ue *UpstreamError
			if !errors.As(err, &ue) {
				t.Fatalf("err = %v", err)
			}
			if ue.Timeout != tt.wantTimeout || ue.ArtifactID == "" || !errors.Is(err, tt.err) {
				t.Errorf("upstream error = %+v", ue)
			}
			a := repo.created[0]
			if a.ResponseStatusCode != nil || a.Response != nil || a.ResponseBodySizeBytes != nil || a.ResponseContentType != nil {
				t.Errorf("response fields must be NULL on failure: %+v", a)
			}
		})
	}
}

func TestExecutePersistsAfterClientCancel(t *testing.T) {
	repo := &fakeRepo{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newTestService(repo, &fakeFetcher{err: context.Canceled}).Execute(
		ctx, ExecuteInput{Method: "GET", URL: "http://example.com"})
	var ue *UpstreamError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v", err)
	}
	if repo.ctxErr != nil || len(repo.created) != 1 {
		t.Fatalf("artifact must be persisted with a live context, ctxErr=%v", repo.ctxErr)
	}
}

func TestExecuteRepositoryError(t *testing.T) {
	_, err := newTestService(&fakeRepo{createErr: errors.New("db down")}, &fakeFetcher{resp: okResponse()}).Execute(
		context.Background(), ExecuteInput{Method: "GET", URL: "http://example.com"})
	var ue *UpstreamError
	if err == nil || errors.As(err, &ue) || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("err = %v", err)
	}
}

func TestExecuteSanitizesBinaryContent(t *testing.T) {
	repo := &fakeRepo{}
	reqBody := "a\x00b"
	_, err := newTestService(repo, &fakeFetcher{resp: &Response{
		StatusCode: 200, ContentType: "application/octet-stream", Body: []byte{'x', 0x00, 0xff, 'y'}, SizeBytes: 4,
	}}).Execute(context.Background(), ExecuteInput{Method: "POST", URL: "http://example.com", Body: &reqBody})
	if err != nil {
		t.Fatal(err)
	}
	a := repo.created[0]
	if *a.Response != "x\uFFFD\uFFFDy" || *a.RequestBody != "a\uFFFDb" {
		t.Errorf("sanitized bodies = %q / %q", *a.Response, *a.RequestBody)
	}
	if *a.ResponseBodySizeBytes != 4 {
		t.Errorf("size must reflect raw bytes, got %d", *a.ResponseBodySizeBytes)
	}
}

func TestExecuteStoresRawResponse(t *testing.T) {
	repo := &fakeRepo{}
	_, err := newTestService(repo, &fakeFetcher{resp: &Response{
		StatusCode: 200,
		Head:       []byte("HTTP/1.1 200 OK\r\nX-Bad: \xff\r\n\r\n"),
		Body:       []byte("body"),
		SizeBytes:  4,
	}}).Execute(context.Background(), ExecuteInput{Method: "GET", URL: "http://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	a := repo.created[0]
	if want := "HTTP/1.1 200 OK\r\nX-Bad: \uFFFD\r\n\r\nbody"; *a.Response != want {
		t.Errorf("response = %q, want %q", *a.Response, want)
	}
	if *a.ResponseBodySizeBytes != 4 {
		t.Errorf("size must count the body only, got %d", *a.ResponseBodySizeBytes)
	}
}

func TestExecuteReportsTruncation(t *testing.T) {
	res, err := newTestService(&fakeRepo{}, &fakeFetcher{resp: &Response{
		StatusCode: 200, Body: []byte("abc"), SizeBytes: 100, Truncated: true,
	}}).Execute(context.Background(), ExecuteInput{Method: "GET", URL: "http://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || *res.Artifact.ResponseBodySizeBytes != 100 {
		t.Errorf("truncation not reported: %+v", res)
	}
}

func TestGetValidatesUUID(t *testing.T) {
	repo := &fakeRepo{}
	svc := newTestService(repo, nil)
	for _, id := range []string{"", "abc", "550e8400e29b41d4a716446655440000", "550e8400-e29b-41d4-a716-44665544000g", "{550e8400-e29b-41d4-a716-446655440000}"} {
		var ve *ValidationError
		if _, err := svc.Get(context.Background(), id); !errors.As(err, &ve) {
			t.Errorf("Get(%q) err = %v, want validation error", id, err)
		}
	}
	if _, err := svc.Get(context.Background(), "550E8400-E29B-41D4-A716-446655440000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
	if repo.getID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("id not normalized: %q", repo.getID)
	}
}

func TestIsUUID(t *testing.T) {
	valid := []string{"550e8400-e29b-41d4-a716-446655440000", "00000000-0000-0000-0000-000000000000", "FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF"}
	invalid := []string{"", "550e8400-e29b-41d4-a716-44665544000", "550e8400-e29b-41d4-a716-4466554400000", "550e8400xe29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-44665544000z"}
	for _, s := range valid {
		if !IsUUID(s) {
			t.Errorf("IsUUID(%q) = false", s)
		}
	}
	for _, s := range invalid {
		if IsUUID(s) {
			t.Errorf("IsUUID(%q) = true", s)
		}
	}
}

func TestSanitizeText(t *testing.T) {
	for in, want := range map[string]string{
		"":              "",
		"plain ascii":   "plain ascii",
		"日本語 ✓":         "日本語 ✓",
		"a\x00b":        "a\uFFFDb",
		"\xff\xfe":      "\uFFFD",
		"ok\xc3":        "ok\uFFFD",
		"\x00\xffmixed": "\uFFFD\uFFFDmixed",
	} {
		if got := sanitizeText([]byte(in)); got != want {
			t.Errorf("sanitizeText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDelete(t *testing.T) {
	repo := &fakeRepo{}
	svc := newTestService(repo, nil)
	if err := svc.Delete(context.Background(), "550E8400-E29B-41D4-A716-446655440000"); err != nil {
		t.Fatal(err)
	}
	if repo.deleteID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("id not normalized: %q", repo.deleteID)
	}
	repo.deleteErr = ErrNotFound
	if err := svc.Delete(context.Background(), "550e8400-e29b-41d4-a716-446655440000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
	repo.deleteID = ""
	var ve *ValidationError
	if err := svc.Delete(context.Background(), "not-a-uuid"); !errors.As(err, &ve) || repo.deleteID != "" {
		t.Errorf("invalid id: err = %v", err)
	}
}

func summaries(n int) []Summary {
	out := make([]Summary, n)
	for i := range out {
		out[i] = Summary{
			ID:          fmt.Sprintf("00000000-0000-0000-0000-%012d", i),
			HTTPMethod:  "GET",
			RequestedAt: time.Date(2026, 1, 1, 0, 0, i, 1000, time.UTC),
		}
	}
	return out
}

func TestListPaging(t *testing.T) {
	p := ListParams{SortBy: "requested_at", SortOrder: SortDesc, Hostname: "a.com", Limit: 2}

	repo := &fakeRepo{listItems: summaries(3)}
	page, err := newTestService(repo, nil).List(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if repo.listP.Limit != 3 {
		t.Errorf("repository must be asked for limit+1, got %d", repo.listP.Limit)
	}
	if len(page.Items) != 2 || page.Next == nil {
		t.Fatalf("page = %+v", page)
	}
	want := ListCursor{SortBy: "requested_at", SortOrder: SortDesc, Hostname: "a.com",
		Value: ptr("2026-01-01T00:00:01.000001Z"), ID: page.Items[1].ID}
	if *page.Next.Value != *want.Value || page.Next.ID != want.ID || page.Next.Hostname != "a.com" ||
		page.Next.SortBy != want.SortBy || page.Next.SortOrder != want.SortOrder {
		t.Errorf("next = %+v", page.Next)
	}

	// Exactly limit rows: last page.
	page, _ = newTestService(&fakeRepo{listItems: summaries(2)}, nil).List(context.Background(), p)
	if len(page.Items) != 2 || page.Next != nil {
		t.Errorf("exact page = %+v", page)
	}
	page, _ = newTestService(&fakeRepo{}, nil).List(context.Background(), p)
	if len(page.Items) != 0 || page.Next != nil {
		t.Errorf("empty page = %+v", page)
	}
}

func TestListRejectsMismatchedCursor(t *testing.T) {
	c := &ListCursor{SortBy: "requested_at", SortOrder: SortDesc, Value: ptr("2026-01-01T00:00:00Z"), ID: "550e8400-e29b-41d4-a716-446655440000"}
	for name, p := range map[string]ListParams{
		"sort_by":    {SortBy: "full_url", SortOrder: SortDesc, Limit: 1, After: c},
		"sort_order": {SortBy: "requested_at", SortOrder: SortAsc, Limit: 1, After: c},
		"hostname":   {SortBy: "requested_at", SortOrder: SortDesc, Hostname: "x.com", Limit: 1, After: c},
	} {
		repo := &fakeRepo{}
		_, err := newTestService(repo, nil).List(context.Background(), p)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Field != "cursor" || repo.listP.Limit != 0 {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	ok := ListParams{SortBy: "requested_at", SortOrder: SortDesc, Limit: 1, After: c}
	if _, err := newTestService(&fakeRepo{}, nil).List(context.Background(), ok); err != nil {
		t.Errorf("matching cursor rejected: %v", err)
	}
}

func TestSortValueRoundTrip(t *testing.T) {
	s := Summary{
		HTTPMethod: "POST", FullURL: "https://a.com/x", ResponseStatusCode: ptr(404),
		ResponseContentType: ptr("text/html"), ResponseBodySizeBytes: ptr(int64(1) << 40),
		RequestedAt: time.Date(2026, 10, 27, 12, 0, 0, 123456000, time.FixedZone("X", 3600)),
	}
	want := map[string]any{
		"http_method": "POST", "full_url": "https://a.com/x", "response_status_code": int64(404),
		"response_content_type": "text/html", "response_body_size_bytes": int64(1) << 40,
		"requested_at": s.RequestedAt,
	}
	for _, field := range ListSortFields {
		v, err := ParseSortValue(field, s.SortValue(field))
		if err != nil {
			t.Fatalf("%s: %v", field, err)
		}
		if tm, ok := v.(time.Time); ok {
			if !tm.Equal(want[field].(time.Time)) {
				t.Errorf("%s: %v", field, tm)
			}
		} else if v != want[field] {
			t.Errorf("%s: got %v (%T), want %v", field, v, v, want[field])
		}
	}
	var empty Summary
	for _, field := range []string{"response_status_code", "response_content_type", "response_body_size_bytes"} {
		if empty.SortValue(field) != nil {
			t.Errorf("%s: NULL must encode as nil", field)
		}
		if v, err := ParseSortValue(field, nil); v != nil || err != nil {
			t.Errorf("%s: nil must parse as NULL", field)
		}
	}
	for _, field := range []string{"http_method", "full_url", "requested_at", "unknown"} {
		if _, err := ParseSortValue(field, nil); err == nil {
			t.Errorf("%s: NULL must be rejected", field)
		}
	}
}
