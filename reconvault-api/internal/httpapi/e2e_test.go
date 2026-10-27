package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/reconvault/reconvault-api/internal/artifact"
	"github.com/reconvault/reconvault-api/internal/fetcher"
	"github.com/reconvault/reconvault-api/internal/httpapi"
	"github.com/reconvault/reconvault-api/internal/postgres"
	"github.com/reconvault/reconvault-api/internal/testdb"
)

const apiKey = "e2e-api-key-0123456789abcdef01234"

type client struct {
	t   *testing.T
	url string
}

func (c client) call(method, path, body string) (int, map[string]any) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.url+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		c.t.Fatalf("%s %s: decode: %v", method, path, err)
	}
	return resp.StatusCode, out
}

func newAPI(t *testing.T, allowPrivate bool) client {
	pool := testdb.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := artifact.NewService(
		postgres.NewArtifactRepository(pool),
		fetcher.New(fetcher.Options{Timeout: 5 * time.Second, MaxBodyBytes: 1 << 20, AllowPrivateNetworks: allowPrivate}),
	)
	srv := httptest.NewServer(httpapi.NewHandler(svc, pool, log, httpapi.Options{
		APIKey: apiKey, MaxRequestBytes: 1 << 20, Recon: postgres.NewReconRepository(pool),
	}))
	t.Cleanup(srv.Close)
	return client{t: t, url: srv.URL}
}

func TestEndToEnd(t *testing.T) {
	var gotUA, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Debug-Token", "HeaderOnlySecret")
		io.WriteString(w, `{"success":true,"api_key":"TopSecretValue"}`)
	}))
	defer upstream.Close()

	c := newAPI(t, true)

	// Execute.
	status, created := c.call("POST", "/api/v1/http/request", `{
		"method": "POST",
		"url": "`+upstream.URL+`/v1/resource?debug=1",
		"headers": {"Content-Type": "application/json", "X-Auth-Token": "tok-987"},
		"body": "{\"id\":123,\"name\":\"test\"}"
	}`)
	if status != http.StatusCreated {
		t.Fatalf("execute: %d %v", status, created)
	}
	id, _ := created["id"].(string)
	if gotUA != artifact.DefaultUserAgent || gotBody != `{"id":123,"name":"test"}` {
		t.Errorf("upstream saw UA=%q body=%q", gotUA, gotBody)
	}
	if created["hostname"] != "127.0.0.1" || created["endpoint_path"] != "/v1/resource" ||
		created["response_status_code"] != 200.0 || created["response_content_type"] != "application/json" ||
		created["response_body_size_bytes"] != 43.0 || created["response_body_truncated"] != false {
		t.Errorf("created = %v", created)
	}

	// Get by ID returns every column.
	status, got := c.call("GET", "/api/v1/http/artifacts/"+id, "")
	if status != http.StatusOK {
		t.Fatalf("get: %d", status)
	}
	for _, k := range []string{"id", "http_method", "hostname", "endpoint_path", "full_url", "requested_at",
		"response_status_code", "response_content_type", "response_body_size_bytes", "request_headers", "request_body", "response"} {
		if _, ok := got[k]; !ok {
			t.Errorf("get: missing field %s", k)
		}
	}
	headers, _ := got["request_headers"].(map[string]any)
	if headers["User-Agent"] != artifact.DefaultUserAgent || headers["X-Auth-Token"] != "tok-987" {
		t.Errorf("request_headers = %v", got["request_headers"])
	}
	if _, err := time.Parse(time.RFC3339Nano, got["requested_at"].(string)); err != nil || !strings.HasSuffix(got["requested_at"].(string), "Z") {
		t.Errorf("requested_at = %v", got["requested_at"])
	}

	// List.
	status, list := c.call("GET", "/api/v1/http/artifacts?hostname=127.0.0.1", "")
	data, _ := list["data"].([]any)
	if status != http.StatusOK || len(data) != 1 {
		t.Fatalf("list: %d %v", status, list)
	}
	item := data[0].(map[string]any)
	if item["id"] != id || len(item) != 7 {
		t.Errorf("list item must have exactly the 7 summary fields: %v", item)
	}

	// Search finds secrets in request headers and response bodies, and
	// returns metadata only.
	raw, _ := got["response"].(string)
	if !strings.HasPrefix(raw, "HTTP/1.1 200 OK\r\n") || !strings.Contains(raw, "\r\nX-Debug-Token: HeaderOnlySecret\r\n") ||
		!strings.HasSuffix(raw, "\r\n\r\n"+`{"success":true,"api_key":"TopSecretValue"}`) {
		t.Errorf("raw response = %q", raw)
	}
	if _, ok := got["response_body"]; ok {
		t.Error("response_body must no longer be returned")
	}

	for _, q := range []string{"tok-987", "topsecretvalue", "x-auth-token", "headeronlysecret", "x-debug-token"} {
		status, res := c.call("GET", "/api/v1/http/artifacts/search?q="+q, "")
		data, _ := res["data"].([]any)
		if status != http.StatusOK || len(data) != 1 {
			t.Fatalf("search %q: %d %v", q, status, res)
		}
		r := data[0].(map[string]any)
		if _, leaked := r["response"]; leaked || r["id"] != id {
			t.Errorf("search result must be metadata only: %v", r)
		}
	}

	// Domains.
	status, domains := c.call("GET", "/api/v1/http/domains", "")
	dd, _ := domains["data"].([]any)
	if status != http.StatusOK || len(dd) != 1 || dd[0].(map[string]any)["artifact_count"] != 1.0 {
		t.Fatalf("domains: %d %v", status, domains)
	}

	// Errors.
	if status, _ := c.call("GET", "/api/v1/http/artifacts/550e8400-e29b-41d4-a716-446655440000", ""); status != http.StatusNotFound {
		t.Errorf("unknown id: %d", status)
	}
	if status, _ := c.call("GET", "/api/v1/http/artifacts/not-a-uuid", ""); status != http.StatusBadRequest {
		t.Errorf("bad uuid: %d", status)
	}
}

func TestEndToEndUpstreamFailureIsRecorded(t *testing.T) {
	c := newAPI(t, true)
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()

	status, body := c.call("POST", "/api/v1/http/request", `{"method":"GET","url":"`+dead.URL+`/x"}`)
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d %v", status, body)
	}
	id, _ := body["error"].(map[string]any)["artifact_id"].(string)
	status, got := c.call("GET", "/api/v1/http/artifacts/"+id, "")
	if status != http.StatusOK || got["response_status_code"] != nil || got["response"] != nil {
		t.Fatalf("failed attempt not recorded correctly: %d %v", status, got)
	}
}

func TestEndToEndSSRFBlocked(t *testing.T) {
	hit := false
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer upstream.Close()
	c := newAPI(t, false)

	status, body := c.call("POST", "/api/v1/http/request", `{"method":"GET","url":"`+upstream.URL+`"}`)
	if status != http.StatusForbidden || hit {
		t.Fatalf("status = %d hit = %v body = %v", status, hit, body)
	}
	_, list := c.call("GET", "/api/v1/http/artifacts", "")
	if data := list["data"].([]any); len(data) != 0 {
		t.Errorf("blocked request must not be stored: %v", data)
	}
}

func TestEndToEndDelete(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	c := newAPI(t, true)

	status, created := c.call("POST", "/api/v1/http/request", `{"method":"GET","url":"`+upstream.URL+`/x"}`)
	if status != http.StatusCreated {
		t.Fatalf("request: %d %v", status, created)
	}
	id := created["id"].(string)

	req, _ := http.NewRequest("DELETE", c.url+"/api/v1/http/artifacts/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || len(b) != 0 {
		t.Fatalf("delete: %d %q", resp.StatusCode, b)
	}
	if status, _ := c.call("GET", "/api/v1/http/artifacts/"+id, ""); status != http.StatusNotFound {
		t.Errorf("get after delete: %d", status)
	}
	if status, _ := c.call("DELETE", "/api/v1/http/artifacts/"+id, ""); status != http.StatusNotFound {
		t.Errorf("second delete: %d", status)
	}
	if status, _ := c.call("DELETE", "/api/v1/http/artifacts/not-a-uuid", ""); status != http.StatusBadRequest {
		t.Errorf("bad uuid: %d", status)
	}
}

func TestEndToEndPagination(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	c := newAPI(t, true)
	created := map[string]bool{}
	for i := range 5 {
		_, body := c.call("POST", "/api/v1/http/request", `{"method":"GET","url":"`+upstream.URL+`/`+string(rune('a'+i))+`"}`)
		created[body["id"].(string)] = true
	}

	seen := map[string]bool{}
	path := "/api/v1/http/artifacts?limit=2&sort_by=full_url&sort_order=ASC"
	var lastURL string
	for pages := 0; ; pages++ {
		status, body := c.call("GET", path, "")
		if status != http.StatusOK || pages > 5 {
			t.Fatalf("page %d: %d %v", pages, status, body)
		}
		for _, item := range body["data"].([]any) {
			m := item.(map[string]any)
			if seen[m["id"].(string)] || m["full_url"].(string) < lastURL {
				t.Fatalf("duplicate or out-of-order row: %v", m)
			}
			seen[m["id"].(string)] = true
			lastURL = m["full_url"].(string)
		}
		next, _ := body["meta"].(map[string]any)["next_cursor"].(string)
		if next == "" {
			break
		}
		path = "/api/v1/http/artifacts?limit=2&cursor=" + next
	}
	if len(seen) != len(created) {
		t.Errorf("walked %d of %d artifacts", len(seen), len(created))
	}
}

func TestEndToEndSelfSignedTarget(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "self-signed ok")
	}))
	defer upstream.Close()
	c := newAPI(t, true)

	status, body := c.call("POST", "/api/v1/http/request", `{"method":"GET","url":"`+upstream.URL+`/"}`)
	if status != http.StatusCreated || body["response_status_code"] != 200.0 || !strings.HasSuffix(body["response"].(string), "self-signed ok") {
		t.Fatalf("self-signed target: %d %v", status, body)
	}
}

func TestEndToEndRecon(t *testing.T) {
	c := newAPI(t, true)
	pool := testdb.New(t)
	testdb.WithReconTables(t, pool)
	ctx := context.Background()
	for _, sql := range []string{
		`INSERT INTO domains (hostname, root_domain, asn, asn_name, waf) VALUES
			('www.example.com', 'example.com', 64500, 'EXAMPLE-NET', false),
			('api.example.com', 'example.com', NULL, NULL, NULL)`,
		`INSERT INTO dns_records (hostname, record_type, record_value, ttl, updated_at) VALUES
			('www.example.com', 'A', '192.0.2.1', 300, NOW() - INTERVAL '1 day'),
			('www.example.com', 'A', '192.0.2.2', 300, NOW())`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}

	status, d := c.call("GET", "/api/v1/domains/www.example.com", "")
	if status != http.StatusOK || d["asn"] != 64500.0 || d["waf"] != false || d["root_domain"] != "example.com" {
		t.Fatalf("domain: %d %v", status, d)
	}
	status, d = c.call("GET", "/api/v1/domains/api.example.com", "")
	if status != http.StatusOK || d["asn"] != nil || d["waf"] != nil {
		t.Fatalf("unknown values must be null: %v", d)
	}
	if status, _ := c.call("GET", "/api/v1/domains/nope.example.com", ""); status != http.StatusNotFound {
		t.Errorf("missing domain: %d", status)
	}

	status, dns := c.call("GET", "/api/v1/dns_records/www.example.com", "")
	rows, _ := dns["data"].([]any)
	if status != http.StatusOK || len(rows) != 2 || rows[0].(map[string]any)["record_value"] != "192.0.2.2" {
		t.Fatalf("dns: %d %v", status, dns)
	}

	status, subs := c.call("GET", "/api/v1/subdomains/example.com", "")
	if status != http.StatusOK || len(subs["data"].([]any)) != 2 || subs["meta"].(map[string]any)["total"] != 2.0 {
		t.Fatalf("subdomains: %d %v", status, subs)
	}

	status, roots := c.call("GET", "/api/v1/root_domains", "")
	first := roots["data"].([]any)[0].(map[string]any)
	if status != http.StatusOK || first["root_domain"] != "example.com" || first["hostname_count"] != 2.0 {
		t.Fatalf("root domains: %d %v", status, roots)
	}
}
