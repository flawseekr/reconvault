package domainscan

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"github.com/reconvault/reconvault-dns-autoscan/internal/netguard"
)

func detector(allowPrivate bool) *WAFDetector {
	return NewWAFDetector(2*time.Second, netguard.Policy{AllowPrivate: allowPrivate})
}

func hostOf(t *testing.T, srv *httptest.Server) string {
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host // 127.0.0.1:port
}

func TestWAFProviderAddressRange(t *testing.T) {
	// 104.16.0.0/13 is a published Cloudflare range (cdncheck's data).
	res := detector(false).Detect(context.Background(), "unreachable.invalid", []netip.Addr{netip.MustParseAddr("104.16.0.1")}, nil)
	if res.Detected == nil || !*res.Detected || res.Provider != "cloudflare" {
		t.Fatalf("result = %+v", res)
	}
}

func TestWAFCNAME(t *testing.T) {
	res := detector(false).Detect(context.Background(), "unreachable.invalid", nil, []string{"x7q.incapdns.net"})
	if res.Detected == nil || !*res.Detected || res.Provider != "Imperva Incapsula" {
		t.Fatalf("result = %+v", res)
	}
	// A CDN-only CNAME is not evidence of a WAF.
	res = detector(false).Detect(context.Background(), "unreachable.invalid", nil, []string{"d111111abcdef8.cloudfront.net"})
	if res.Detected != nil {
		t.Fatalf("CDN CNAME produced %+v", res)
	}
}

func TestWAFHTTPFingerprints(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		cookie   string
		want     bool
		provider string
	}{
		{"cloudflare ray", map[string]string{"CF-RAY": "8a1b2c3d4e5f-SIN"}, "", true, "Cloudflare"},
		{"sucuri", map[string]string{"X-Sucuri-ID": "11005"}, "", true, "Sucuri"},
		{"incapsula cookie", nil, "incap_ses_123_456", true, "Imperva Incapsula"},
		{"aws waf", map[string]string{"X-Amzn-Waf-Action": "captcha"}, "", true, "AWS WAF"},
		{"akamai CDN alone", map[string]string{"Server": "AkamaiGHost"}, "", false, ""},
		{"plain nginx", map[string]string{"Server": "nginx"}, "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}
				if tt.cookie != "" {
					http.SetCookie(w, &http.Cookie{Name: tt.cookie, Value: "x"})
				}
			}))
			defer srv.Close()
			res := detector(true).Detect(context.Background(), hostOf(t, srv), nil, nil)
			if res.Detected == nil || *res.Detected != tt.want || res.Provider != tt.provider {
				t.Fatalf("result = %+v", res)
			}
		})
	}
}

func TestWAFInconclusiveWhenUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	host := hostOf(t, srv)
	srv.Close()
	if res := detector(true).Detect(context.Background(), host, nil, nil); res.Detected != nil {
		t.Fatalf("unreachable host must be inconclusive: %+v", res)
	}
}

func TestWAFProbeRespectsAddressPolicy(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.Header().Set("CF-RAY", "x")
	}))
	defer srv.Close()
	res := detector(false).Detect(context.Background(), hostOf(t, srv), nil, nil)
	if hit || res.Detected != nil {
		t.Fatalf("loopback probe allowed: hit=%v result=%+v", hit, res)
	}
}
