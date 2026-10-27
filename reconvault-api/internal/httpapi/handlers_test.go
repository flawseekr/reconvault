package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/reconvault/reconvault-api/internal/artifact"
)

const testKey = "test-api-key-0123456789abcdef0123"

type fakeService struct {
	executeIn  artifact.ExecuteInput
	deleteID   string
	listPage   *artifact.ListPage
	executeRes *artifact.ExecuteResult
	listP      artifact.ListParams
	searchP    artifact.SearchParams
	domainP    artifact.DomainParams
	getID      string
	err        error
	panicMsg   string
}

func (f *fakeService) Execute(_ context.Context, in artifact.ExecuteInput) (*artifact.ExecuteResult, error) {
	f.executeIn = in
	return f.executeRes, f.err
}
func (f *fakeService) Delete(_ context.Context, id string) error {
	f.deleteID = id
	return f.err
}
func (f *fakeService) Get(_ context.Context, id string) (*artifact.Artifact, error) {
	if f.panicMsg != "" {
		panic(f.panicMsg)
	}
	f.getID = id
	if f.err != nil {
		return nil, f.err
	}
	return &artifact.Artifact{ID: id, RequestHeaders: json.RawMessage(`{"A":"b"}`)}, nil
}
func (f *fakeService) List(_ context.Context, p artifact.ListParams) (*artifact.ListPage, error) {
	f.listP = p
	if f.err != nil {
		return nil, f.err
	}
	if f.listPage != nil {
		return f.listPage, nil
	}
	return &artifact.ListPage{}, nil
}
func (f *fakeService) Search(_ context.Context, p artifact.SearchParams) ([]artifact.SearchResult, error) {
	f.searchP = p
	return nil, f.err
}
func (f *fakeService) Domains(_ context.Context, p artifact.DomainParams) ([]artifact.Domain, error) {
	f.domainP = p
	return []artifact.Domain{{Hostname: "a.com", ArtifactCount: 2, LastRequestedAt: time.Unix(0, 0).UTC()}}, f.err
}

type fakePinger struct{ err error }

func (p fakePinger) Ping(context.Context) error { return p.err }

func newTestHandler(svc ArtifactService, db Pinger) http.Handler {
	return NewHandler(svc, db, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{APIKey: testKey, MaxRequestBytes: 1024})
}

func do(t *testing.T, h http.Handler, method, target, body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testKey)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, m := range mutate {
		m(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorDetail {
	t.Helper()
	var b errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return b.Error
}

func TestAuthentication(t *testing.T) {
	h := newTestHandler(&fakeService{}, fakePinger{})
	tests := []struct {
		name, header string
		want         int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong key", "Bearer wrong", http.StatusUnauthorized},
		{"key prefix", "Bearer " + testKey[:10], http.StatusUnauthorized},
		{"key with suffix", "Bearer " + testKey + "x", http.StatusUnauthorized},
		{"wrong scheme", "Basic " + testKey, http.StatusUnauthorized},
		{"no token", "Bearer ", http.StatusUnauthorized},
		{"scheme only", "Bearer", http.StatusUnauthorized},
		{"valid", "Bearer " + testKey, http.StatusOK},
		{"case-insensitive scheme", "bearer " + testKey, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, h, "GET", "/api/v1/http/artifacts", "", func(r *http.Request) {
				r.Header.Set("Authorization", tt.header)
			})
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			if tt.want == http.StatusUnauthorized {
				if decodeError(t, rec).Code != codeUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
					t.Error("missing unauthorized body or WWW-Authenticate")
				}
			}
		})
	}
}

func TestUnknownRoutesRequireAuthAndReturnJSON404(t *testing.T) {
	h := newTestHandler(&fakeService{}, fakePinger{})
	rec := do(t, h, "GET", "/api/v1/unknown", "", func(r *http.Request) { r.Header.Del("Authorization") })
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated unknown route: %d", rec.Code)
	}
	rec = do(t, h, "GET", "/api/v1/unknown", "")
	if rec.Code != http.StatusNotFound || decodeError(t, rec).Code != codeNotFound {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
}

// TestLegacyV1PrefixIsGone guards the /v1 -> /api/v1 migration: the old
// paths must not route anywhere.
func TestLegacyV1PrefixIsGone(t *testing.T) {
	h := newTestHandler(&fakeService{}, fakePinger{})
	for _, path := range []string{"/v1/http/artifacts", "/v1/http/domains", "/v1/root_domains"} {
		if rec := do(t, h, "GET", path, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d", path, rec.Code)
		}
	}
	if rec := do(t, h, "GET", "/api/v1/http/artifacts", ""); rec.Code != http.StatusOK {
		t.Errorf("/api/v1/http/artifacts: status %d", rec.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := newTestHandler(&fakeService{}, fakePinger{})
	rec := do(t, h, "GET", "/api/v1/http/request", "")
	if rec.Code != http.StatusMethodNotAllowed || decodeError(t, rec).Code != codeMethodNotAllowed {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	if allow := rec.Header().Get("Allow"); allow != "POST" {
		t.Errorf("Allow = %q", allow)
	}
	rec = do(t, h, "PUT", "/api/v1/http/artifacts/550e8400-e29b-41d4-a716-446655440000", "")
	if rec.Code != http.StatusMethodNotAllowed || !strings.Contains(rec.Header().Get("Allow"), "GET") {
		t.Fatalf("status = %d Allow = %q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestHealthEndpoints(t *testing.T) {
	h := newTestHandler(&fakeService{}, fakePinger{})
	for _, path := range []string{"/healthz", "/readyz"} {
		rec := do(t, h, "GET", path, "", func(r *http.Request) { r.Header.Del("Authorization") })
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d", path, rec.Code)
		}
	}
	h = newTestHandler(&fakeService{}, fakePinger{err: errors.New("down")})
	if rec := do(t, h, "GET", "/readyz", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("readyz with db down = %d", rec.Code)
	}
	if rec := do(t, h, "GET", "/healthz", ""); rec.Code != http.StatusOK {
		t.Errorf("healthz must not depend on db, got %d", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := do(t, newTestHandler(&fakeService{}, fakePinger{}), "GET", "/healthz", "")
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "no-store",
		"Content-Type":           "application/json; charset=utf-8",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestPanicRecovery(t *testing.T) {
	rec := do(t, newTestHandler(&fakeService{panicMsg: "boom"}, fakePinger{}), "GET",
		"/api/v1/http/artifacts/550e8400-e29b-41d4-a716-446655440000", "")
	if rec.Code != http.StatusInternalServerError || decodeError(t, rec).Code != codeInternal {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
}

func TestExecuteRequestSuccess(t *testing.T) {
	code := 200
	svc := &fakeService{executeRes: &artifact.ExecuteResult{
		Artifact:  &artifact.Artifact{ID: "550e8400-e29b-41d4-a716-446655440000", HTTPMethod: "POST", ResponseStatusCode: &code},
		Truncated: true,
	}}
	body := `{"method":"POST","url":"https://api.example.com/v1/resource","headers":{"Content-Type":"application/json"},"body":"{\"id\":123}"}`
	rec := do(t, newTestHandler(svc, fakePinger{}), "POST", "/api/v1/http/request", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/api/v1/http/artifacts/550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("Location = %q", loc)
	}
	in := svc.executeIn
	if in.Method != "POST" || in.URL != "https://api.example.com/v1/resource" || in.Headers["Content-Type"] != "application/json" || *in.Body != `{"id":123}` {
		t.Errorf("input = %+v", in)
	}
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["id"] != "550e8400-e29b-41d4-a716-446655440000" || got["response_body_truncated"] != true || got["response_status_code"] != 200.0 {
		t.Errorf("body = %v", got)
	}
	if _, ok := got["request_headers"]; !ok {
		t.Error("all artifact fields must be present")
	}
}

func TestExecuteRequestOptionalFields(t *testing.T) {
	svc := &fakeService{executeRes: &artifact.ExecuteResult{Artifact: &artifact.Artifact{ID: "x"}}}
	rec := do(t, newTestHandler(svc, fakePinger{}), "POST", "/api/v1/http/request", `{"method":"GET","url":"http://a.com","headers":null,"body":null}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	if svc.executeIn.Headers != nil || svc.executeIn.Body != nil {
		t.Errorf("input = %+v", svc.executeIn)
	}
}

func TestExecuteRequestBadInput(t *testing.T) {
	tests := []struct {
		name, body, contentType string
		want                    int
		code                    string
	}{
		{"no content type", `{}`, "-", http.StatusUnsupportedMediaType, codeUnsupportedMediaType},
		{"text content type", `{}`, "text/plain", http.StatusUnsupportedMediaType, codeUnsupportedMediaType},
		{"empty body", ``, "application/json", http.StatusBadRequest, codeBadRequest},
		{"malformed json", `{"method":`, "application/json", http.StatusBadRequest, codeBadRequest},
		{"syntax error", `{"method" "GET"}`, "application/json", http.StatusBadRequest, codeBadRequest},
		{"not an object", `[1,2]`, "application/json", http.StatusBadRequest, codeBadRequest},
		{"wrong field type", `{"method":1,"url":"http://a"}`, "application/json", http.StatusBadRequest, codeBadRequest},
		{"non-string header value", `{"method":"GET","url":"http://a","headers":{"X":1}}`, "application/json", http.StatusBadRequest, codeBadRequest},
		{"unknown field", `{"method":"GET","url":"http://a","extra":1}`, "application/json", http.StatusBadRequest, codeBadRequest},
		{"removed TLS toggle", `{"method":"GET","url":"http://a","ignore_tls_errors":true}`, "application/json", http.StatusBadRequest, codeBadRequest},
		{"trailing data", `{"method":"GET","url":"http://a"} {}`, "application/json", http.StatusBadRequest, codeBadRequest},
		{"too large", `{"method":"GET","url":"http://a","body":"` + strings.Repeat("a", 2048) + `"}`, "application/json", http.StatusRequestEntityTooLarge, codePayloadTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{}
			rec := do(t, newTestHandler(svc, fakePinger{}), "POST", "/api/v1/http/request", tt.body, func(r *http.Request) {
				if tt.contentType == "-" {
					r.Header.Del("Content-Type")
				} else {
					r.Header.Set("Content-Type", tt.contentType)
				}
			})
			if rec.Code != tt.want || decodeError(t, rec).Code != tt.code {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
			}
			if svc.executeIn.URL != "" {
				t.Error("service must not be called")
			}
		})
	}
}

func TestExecuteRequestAcceptsJSONWithCharset(t *testing.T) {
	svc := &fakeService{executeRes: &artifact.ExecuteResult{Artifact: &artifact.Artifact{ID: "x"}}}
	rec := do(t, newTestHandler(svc, fakePinger{}), "POST", "/api/v1/http/request", `{"method":"GET","url":"http://a.com"}`,
		func(r *http.Request) { r.Header.Set("Content-Type", "Application/JSON; charset=utf-8") })
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestServiceErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		want       int
		code       string
		artifactID string
	}{
		{"validation", &artifact.ValidationError{Field: "url", Message: "bad"}, http.StatusBadRequest, codeValidation, ""},
		{"blocked", artifact.ErrDestinationNotAllowed, http.StatusForbidden, codeDestinationNotAllowed, ""},
		{"upstream", &artifact.UpstreamError{ArtifactID: "id1", Err: errors.New("dial tcp 10.0.0.1: refused")}, http.StatusBadGateway, codeUpstreamFailed, "id1"},
		{"upstream timeout", &artifact.UpstreamError{ArtifactID: "id2", Timeout: true, Err: errors.New("timeout")}, http.StatusGatewayTimeout, codeUpstreamTimeout, "id2"},
		{"internal", errors.New("pq: secret internals"), http.StatusInternalServerError, codeInternal, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, newTestHandler(&fakeService{err: tt.err}, fakePinger{}), "POST", "/api/v1/http/request", `{"method":"GET","url":"http://a.com"}`)
			d := decodeError(t, rec)
			if rec.Code != tt.want || d.Code != tt.code || d.ArtifactID != tt.artifactID {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
			}
			if strings.Contains(rec.Body.String(), "10.0.0.1") || strings.Contains(rec.Body.String(), "secret internals") {
				t.Error("internal error details leaked")
			}
		})
	}
}

func TestGetArtifact(t *testing.T) {
	svc := &fakeService{}
	h := newTestHandler(svc, fakePinger{})
	rec := do(t, h, "GET", "/api/v1/http/artifacts/550e8400-e29b-41d4-a716-446655440000", "")
	if rec.Code != http.StatusOK || svc.getID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("status = %d id = %q", rec.Code, svc.getID)
	}
	if !strings.Contains(rec.Body.String(), `"request_headers":{"A":"b"}`) {
		t.Errorf("request_headers must be embedded JSON: %s", rec.Body)
	}

	svc.err = artifact.ErrNotFound
	if rec := do(t, h, "GET", "/api/v1/http/artifacts/550e8400-e29b-41d4-a716-446655440001", ""); rec.Code != http.StatusNotFound {
		t.Errorf("not found status = %d", rec.Code)
	}
}

func TestSearchRouteTakesPrecedenceOverID(t *testing.T) {
	svc := &fakeService{}
	rec := do(t, newTestHandler(svc, fakePinger{}), "GET", "/api/v1/http/artifacts/search?q=secret", "")
	if rec.Code != http.StatusOK || svc.searchP.Query != "secret" || svc.getID != "" {
		t.Fatalf("status = %d search = %+v get = %q", rec.Code, svc.searchP, svc.getID)
	}
}

func TestListArtifactsParams(t *testing.T) {
	tests := []struct {
		query string
		want  artifact.ListParams
	}{
		{"", artifact.ListParams{SortBy: "requested_at", SortOrder: "DESC", Limit: 30}},
		{"?hostname=API.Example.com", artifact.ListParams{SortBy: "requested_at", SortOrder: "DESC", Limit: 30, Hostname: "api.example.com"}},
		{"?sort_by=response_status_code&sort_order=ASC", artifact.ListParams{SortBy: "response_status_code", SortOrder: "ASC", Limit: 30}},
		{"?sort_order=asc&limit=1", artifact.ListParams{SortBy: "requested_at", SortOrder: "ASC", Limit: 1}},
		{"?limit=100", artifact.ListParams{SortBy: "requested_at", SortOrder: "DESC", Limit: 100}},
		{"?hostname=api.example.com&sort_by=requested_at&sort_order=DESC&limit=10", artifact.ListParams{SortBy: "requested_at", SortOrder: "DESC", Limit: 10, Hostname: "api.example.com"}},
	}
	for _, sortBy := range artifact.ListSortFields {
		tests = append(tests, struct {
			query string
			want  artifact.ListParams
		}{"?sort_by=" + sortBy, artifact.ListParams{SortBy: sortBy, SortOrder: "DESC", Limit: 30}})
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			svc := &fakeService{}
			rec := do(t, newTestHandler(svc, fakePinger{}), "GET", "/api/v1/http/artifacts"+tt.query, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
			}
			if svc.listP != tt.want {
				t.Errorf("params = %+v, want %+v", svc.listP, tt.want)
			}
			if !strings.HasPrefix(rec.Body.String(), `{"data":[]`) {
				t.Errorf("empty data must encode as []: %s", rec.Body)
			}
		})
	}
}

func TestListArtifactsInvalidParams(t *testing.T) {
	for _, q := range []string{
		"?sort_by=hostname", "?sort_by=id%3BDROP%20TABLE%20x", "?sort_by=", "?sort_order=up", "?sort_order=",
		"?limit=0", "?limit=-1", "?limit=101", "?limit=abc", "?limit=", "?limit=1.5", "?hostname=", "?hostname=%20",
		"?hostname=" + strings.Repeat("a", maxHostnameLength+1),
	} {
		t.Run(q, func(t *testing.T) {
			rec := do(t, newTestHandler(&fakeService{}, fakePinger{}), "GET", "/api/v1/http/artifacts"+q, "")
			if rec.Code != http.StatusBadRequest || decodeError(t, rec).Code != codeValidation {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestListArtifactsMeta(t *testing.T) {
	rec := do(t, newTestHandler(&fakeService{}, fakePinger{}), "GET", "/api/v1/http/artifacts?hostname=a.com&limit=5", "")
	want := `{"data":[],"meta":{"sort_by":"requested_at","sort_order":"DESC","hostname":"a.com","limit":5,"next_cursor":null}}`
	if strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("body = %s", rec.Body)
	}
}

func TestSearchParams(t *testing.T) {
	svc := &fakeService{}
	h := newTestHandler(svc, fakePinger{})
	rec := do(t, h, "GET", "/api/v1/http/artifacts/search?q=%20Secret%25_&limit=5", "")
	if rec.Code != http.StatusOK || svc.searchP != (artifact.SearchParams{Query: " Secret%_", Limit: 5}) {
		t.Fatalf("status = %d params = %+v", rec.Code, svc.searchP)
	}
	want := `{"data":[],"meta":{"query":" Secret%_","limit":5}}`
	if strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("body = %s", rec.Body)
	}

	do(t, h, "GET", "/api/v1/http/artifacts/search?q=x", "")
	if svc.searchP.Limit != 30 {
		t.Errorf("default limit = %d", svc.searchP.Limit)
	}
	do(t, h, "GET", "/api/v1/http/artifacts/search?q="+strings.Repeat("a", maxQueryLength), "")
	if len(svc.searchP.Query) != maxQueryLength {
		t.Error("max-length query rejected")
	}
}

func TestSearchInvalidParams(t *testing.T) {
	for _, q := range []string{
		"", "?q=", "?q=%20%20", "?q=%09%0A", "?limit=5", "?q=a%1Fb", "?q=a%00", "?q=%FF",
		"?q=x&limit=0", "?q=x&limit=101", "?q=x&limit=x", "?q=" + strings.Repeat("a", maxQueryLength+1),
	} {
		t.Run(q, func(t *testing.T) {
			rec := do(t, newTestHandler(&fakeService{}, fakePinger{}), "GET", "/api/v1/http/artifacts/search"+q, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestDomainsParams(t *testing.T) {
	tests := []struct {
		query string
		want  artifact.DomainParams
	}{
		{"", artifact.DomainParams{SortBy: "latest_update", SortOrder: "DESC", Limit: 100}},
		{"?sort_by=total_artifacts&sort_order=ASC", artifact.DomainParams{SortBy: "total_artifacts", SortOrder: "ASC", Limit: 100}},
		{"?sort_by=latest_update&sort_order=asc&limit=1000", artifact.DomainParams{SortBy: "latest_update", SortOrder: "ASC", Limit: 1000}},
	}
	for _, tt := range tests {
		svc := &fakeService{}
		rec := do(t, newTestHandler(svc, fakePinger{}), "GET", "/api/v1/http/domains"+tt.query, "")
		if rec.Code != http.StatusOK || svc.domainP != tt.want {
			t.Errorf("%s: status = %d params = %+v", tt.query, rec.Code, svc.domainP)
		}
	}
	rec := do(t, newTestHandler(&fakeService{}, fakePinger{}), "GET", "/api/v1/http/domains", "")
	want := `{"data":[{"hostname":"a.com","artifact_count":2,"last_requested_at":"1970-01-01T00:00:00Z"}],"meta":{"sort_by":"latest_update","sort_order":"DESC","limit":100}}`
	if strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("body = %s", rec.Body)
	}
	for _, q := range []string{"?sort_by=requested_at", "?sort_by=hostname", "?sort_order=x", "?limit=0", "?limit=1001"} {
		if rec := do(t, newTestHandler(&fakeService{}, fakePinger{}), "GET", "/api/v1/http/domains"+q, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", q, rec.Code)
		}
	}
}

func TestReadEndpointsInternalError(t *testing.T) {
	for _, path := range []string{"/api/v1/http/artifacts", "/api/v1/http/artifacts/search?q=x", "/api/v1/http/domains"} {
		rec := do(t, newTestHandler(&fakeService{err: errors.New("db")}, fakePinger{}), "GET", path, "")
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status = %d", path, rec.Code)
		}
	}
}

func TestDeleteArtifact(t *testing.T) {
	svc := &fakeService{}
	h := newTestHandler(svc, fakePinger{})
	rec := do(t, h, "DELETE", "/api/v1/http/artifacts/550e8400-e29b-41d4-a716-446655440000", "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 || svc.deleteID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("status = %d body = %q id = %q", rec.Code, rec.Body, svc.deleteID)
	}
	for _, tt := range []struct {
		err  error
		want int
	}{
		{artifact.ErrNotFound, http.StatusNotFound},
		{&artifact.ValidationError{Field: "id", Message: "must be a valid UUID"}, http.StatusBadRequest},
		{errors.New("db"), http.StatusInternalServerError},
	} {
		rec := do(t, newTestHandler(&fakeService{err: tt.err}, fakePinger{}), "DELETE", "/api/v1/http/artifacts/x", "")
		if rec.Code != tt.want {
			t.Errorf("%v: status = %d", tt.err, rec.Code)
		}
	}
	rec = do(t, h, "DELETE", "/api/v1/http/artifacts/550e8400-e29b-41d4-a716-446655440000", "",
		func(r *http.Request) { r.Header.Del("Authorization") })
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated delete: status = %d", rec.Code)
	}
}

func TestListNextCursorRoundTrip(t *testing.T) {
	next := &artifact.ListCursor{
		SortBy: "response_status_code", SortOrder: "ASC", Hostname: "a.com",
		Value: ptr("404"), ID: "550e8400-e29b-41d4-a716-446655440000",
	}
	svc := &fakeService{listPage: &artifact.ListPage{Next: next}}
	h := newTestHandler(svc, fakePinger{})
	rec := do(t, h, "GET", "/api/v1/http/artifacts?sort_by=response_status_code&sort_order=ASC&hostname=a.com", "")
	var body struct {
		Meta struct {
			NextCursor *string `json:"next_cursor"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Meta.NextCursor == nil {
		t.Fatalf("next_cursor missing: %s", rec.Body)
	}

	// Following the cursor alone restores sort, order and hostname.
	do(t, h, "GET", "/api/v1/http/artifacts?limit=5&cursor="+*body.Meta.NextCursor, "")
	got := svc.listP
	if got.SortBy != "response_status_code" || got.SortOrder != "ASC" || got.Hostname != "a.com" || got.Limit != 5 ||
		got.After == nil || got.After.ID != next.ID || got.After.Value == nil || *got.After.Value != "404" ||
		got.After.SortBy != next.SortBy || got.After.SortOrder != next.SortOrder || got.After.Hostname != next.Hostname {
		t.Errorf("params = %+v after = %+v", got, got.After)
	}

	// Explicit parameters are passed through for the service to check.
	do(t, h, "GET", "/api/v1/http/artifacts?sort_order=DESC&cursor="+*body.Meta.NextCursor, "")
	if svc.listP.SortOrder != "DESC" || svc.listP.After.SortOrder != "ASC" {
		t.Errorf("explicit sort_order not preserved: %+v", svc.listP)
	}
}

func TestListInvalidCursor(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	id := "550e8400-e29b-41d4-a716-446655440000"
	for name, c := range map[string]string{
		"empty":         "",
		"not base64":    "!!!",
		"not json":      enc("nope"),
		"unknown sort":  enc(`{"s":"hostname","o":"ASC","v":"x","i":"` + id + `"}`),
		"bad order":     enc(`{"s":"full_url","o":"UP","v":"x","i":"` + id + `"}`),
		"bad id":        enc(`{"s":"full_url","o":"ASC","v":"x","i":"1"}`),
		"null not-null": enc(`{"s":"requested_at","o":"ASC","v":null,"i":"` + id + `"}`),
		"bad time":      enc(`{"s":"requested_at","o":"ASC","v":"yesterday","i":"` + id + `"}`),
		"bad int":       enc(`{"s":"response_status_code","o":"ASC","v":"4o4","i":"` + id + `"}`),
		"too long":      strings.Repeat("A", maxCursorLength+1),
	} {
		t.Run(name, func(t *testing.T) {
			rec := do(t, newTestHandler(&fakeService{}, fakePinger{}), "GET", "/api/v1/http/artifacts?cursor="+c, "")
			if rec.Code != http.StatusBadRequest || decodeError(t, rec).Field != "cursor" {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
			}
		})
	}
}
