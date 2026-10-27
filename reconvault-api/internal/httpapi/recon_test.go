package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/reconvault/reconvault-api/internal/recon"
)

type fakeRecon struct {
	host, root string
	limit      int
	err        error
}

func (f *fakeRecon) Domain(_ context.Context, host string) (*recon.Domain, error) {
	f.host = host
	if f.err != nil {
		return nil, f.err
	}
	asn, name, waf := int64(13335), "CLOUDFLARENET", true
	return &recon.Domain{Hostname: host, RootDomain: "example.com", ASN: &asn, ASNName: &name, WAF: &waf, UpdatedAt: time.Unix(0, 0).UTC()}, nil
}
func (f *fakeRecon) DNSHistory(_ context.Context, host string) ([]recon.DNSRecord, error) {
	f.host = host
	if f.err != nil {
		return nil, f.err
	}
	ttl := int32(300)
	return []recon.DNSRecord{
		{Hostname: host, RecordType: "A", RecordValue: "192.0.2.2", TTL: &ttl, UpdatedAt: time.Unix(60, 0).UTC()},
		{Hostname: host, RecordType: "A", RecordValue: "192.0.2.1", UpdatedAt: time.Unix(0, 0).UTC()},
	}, nil
}
func (f *fakeRecon) Subdomains(_ context.Context, root string) ([]recon.Subdomain, error) {
	f.root = root
	if f.err != nil {
		return nil, f.err
	}
	if root == "empty.test" {
		return nil, nil
	}
	return []recon.Subdomain{{Hostname: "api." + root}, {Hostname: "www." + root}}, nil
}
func (f *fakeRecon) RootDomains(_ context.Context, limit int) ([]recon.RootDomain, error) {
	f.limit = limit
	if f.err != nil {
		return nil, f.err
	}
	return []recon.RootDomain{{RootDomain: "example.com", HostnameCount: 2}}, nil
}

func reconHandler(r *fakeRecon) http.Handler {
	return NewHandler(&fakeService{}, fakePinger{}, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{APIKey: testKey, MaxRequestBytes: 1024, Recon: r})
}

func TestGetDomain(t *testing.T) {
	r := &fakeRecon{}
	rec := do(t, reconHandler(r), "GET", "/api/v1/domains/WWW.Example.COM", "")
	if rec.Code != http.StatusOK || r.host != "www.example.com" {
		t.Fatalf("status %d host %q", rec.Code, r.host)
	}
	want := `{"hostname":"www.example.com","root_domain":"example.com","asn":13335,"asn_name":"CLOUDFLARENET","waf":true,"updated_at":"1970-01-01T00:00:00Z"}`
	if strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("body = %s", rec.Body)
	}

	rec = do(t, reconHandler(&fakeRecon{err: recon.ErrNotFound}), "GET", "/api/v1/domains/missing.test", "")
	if rec.Code != http.StatusNotFound || decodeError(t, rec).Message != "domain not found" {
		t.Errorf("missing: %d %s", rec.Code, rec.Body)
	}
}

func TestDNSRecordsEnvelope(t *testing.T) {
	r := &fakeRecon{}
	rec := do(t, reconHandler(r), "GET", "/api/v1/dns_records/H.test", "")
	if rec.Code != http.StatusOK || r.host != "h.test" {
		t.Fatalf("status %d host %q", rec.Code, r.host)
	}
	var body struct {
		Data []map[string]any `json:"data"`
		Meta map[string]any   `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 2 || body.Meta["total"] != 2.0 || body.Meta["hostname"] != "h.test" {
		t.Errorf("body = %s", rec.Body)
	}
	if body.Data[1]["ttl"] != nil || body.Data[0]["record_value"] != "192.0.2.2" {
		t.Errorf("rows = %v", body.Data)
	}
}

func TestSubdomainsEnvelope(t *testing.T) {
	r := &fakeRecon{}
	rec := do(t, reconHandler(r), "GET", "/api/v1/subdomains/Example.com", "")
	want := `{"data":[{"hostname":"api.example.com"},{"hostname":"www.example.com"}],"meta":{"root_domain":"example.com","total":2}}`
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	rec = do(t, reconHandler(r), "GET", "/api/v1/subdomains/empty.test", "")
	if strings.TrimSpace(rec.Body.String()) != `{"data":[],"meta":{"root_domain":"empty.test","total":0}}` {
		t.Errorf("empty: %s", rec.Body)
	}
}

func TestRootDomains(t *testing.T) {
	r := &fakeRecon{}
	rec := do(t, reconHandler(r), "GET", "/api/v1/root_domains", "")
	want := `{"data":[{"root_domain":"example.com","hostname_count":2}],"meta":{"limit":100}}`
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want || r.limit != 100 {
		t.Fatalf("status %d body %s limit %d", rec.Code, rec.Body, r.limit)
	}
	if do(t, reconHandler(r), "GET", "/api/v1/root_domains?limit=1000", ""); r.limit != 1000 {
		t.Errorf("limit = %d", r.limit)
	}
	for _, q := range []string{"limit=0", "limit=1001", "limit=x"} {
		if rec := do(t, reconHandler(r), "GET", "/api/v1/root_domains?"+q, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, rec.Code)
		}
	}
}

func TestReconValidationAndErrors(t *testing.T) {
	long := strings.Repeat("a", maxHostnameLength+1)
	for _, path := range []string{"/api/v1/domains/", "/api/v1/dns_records/", "/api/v1/subdomains/"} {
		for _, bad := range []string{long, "%20%20", "a%01b"} {
			rec := do(t, reconHandler(&fakeRecon{}), "GET", path+bad, "")
			if rec.Code != http.StatusBadRequest || decodeError(t, rec).Code != codeValidation {
				t.Errorf("%s%s: %d %s", path, bad[:min(len(bad), 10)], rec.Code, rec.Body)
			}
		}
		// The boundary length is accepted.
		if rec := do(t, reconHandler(&fakeRecon{}), "GET", path+strings.Repeat("a", maxHostnameLength), ""); rec.Code != http.StatusOK {
			t.Errorf("%s at max length: %d", path, rec.Code)
		}
		failing := &fakeRecon{err: errors.New("pq: connection reset")}
		if rec := do(t, reconHandler(failing), "GET", path+"a.test", ""); rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "pq:") {
			t.Errorf("%s internal error: %d %s", path, rec.Code, rec.Body)
		}
		unauth := do(t, reconHandler(&fakeRecon{}), "GET", path+"a.test", "", func(r *http.Request) { r.Header.Del("Authorization") })
		if unauth.Code != http.StatusUnauthorized {
			t.Errorf("%s without key: %d", path, unauth.Code)
		}
	}
}
