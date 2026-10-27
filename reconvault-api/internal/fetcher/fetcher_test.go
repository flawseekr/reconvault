package fetcher

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/reconvault/reconvault-api/internal/artifact"
)

func outbound(t *testing.T, method, rawURL string, headers http.Header, body *string) artifact.OutboundRequest {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	if headers == nil {
		headers = http.Header{}
	}
	return artifact.OutboundRequest{Method: method, URL: u, Headers: headers, Body: body}
}

func allowAll(maxBody int64) *Fetcher {
	return New(Options{Timeout: 5 * time.Second, MaxBodyBytes: maxBody, AllowPrivateNetworks: true})
}

func TestDoSendsRequestAsSpecified(t *testing.T) {
	var got *http.Request
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"success":true}`)
	}))
	defer srv.Close()

	body := `{"id":123}`
	h := http.Header{"Content-Type": {"application/json"}, "User-Agent": {"ua/1"}, "X-Secret": {"s3cr3t"}}
	resp, err := allowAll(1<<20).Do(context.Background(), outbound(t, "POST", srv.URL+"/v1/resource?q=1", h, &body))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got.Method != "POST" || got.URL.Path != "/v1/resource" || got.URL.RawQuery != "q=1" || gotBody != body {
		t.Errorf("request mismatch: %s %s %q", got.Method, got.URL, gotBody)
	}
	if got.Header.Get("User-Agent") != "ua/1" || got.Header.Get("X-Secret") != "s3cr3t" || got.Header.Get("Content-Type") != "application/json" {
		t.Errorf("headers not forwarded: %v", got.Header)
	}
	if got.Header.Get("Accept-Encoding") != "" {
		t.Errorf("transport must not add Accept-Encoding, got %q", got.Header.Get("Accept-Encoding"))
	}
	if resp.StatusCode != 201 || resp.ContentType != "application/json" || string(resp.Body) != `{"success":true}` || resp.SizeBytes != 16 || resp.Truncated {
		t.Errorf("response = %+v", resp)
	}
}

func TestDoHostHeaderOverride(t *testing.T) {
	var host string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { host = r.Host }))
	defer srv.Close()
	_, err := allowAll(1024).Do(context.Background(), outbound(t, "GET", srv.URL, http.Header{"Host": {"virtual.example"}}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if host != "virtual.example" {
		t.Errorf("Host = %q", host)
	}
}

func TestDoDoesNotFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			t.Error("redirect was followed")
		}
		http.Redirect(w, r, "/target", http.StatusFound)
	}))
	defer srv.Close()
	resp, err := allowAll(1024).Do(context.Background(), outbound(t, "GET", srv.URL, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestDoTruncatesLargeBodies(t *testing.T) {
	const limit = 1024
	tests := []struct {
		name          string
		size          int
		chunked       bool
		wantTruncated bool
		wantSize      int64
	}{
		{"below limit", limit - 1, false, false, limit - 1},
		{"exactly limit", limit, false, false, limit},
		{"one over limit with content-length", limit + 1, false, true, limit + 1},
		{"far over limit with content-length", 10 * limit, false, true, 10 * limit},
		{"over limit chunked", 10 * limit, true, true, limit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if !tt.chunked {
					w.Header().Set("Content-Length", strconv.Itoa(tt.size))
				}
				w.Write([]byte(strings.Repeat("a", tt.size)))
			}))
			defer srv.Close()
			resp, err := allowAll(limit).Do(context.Background(), outbound(t, "GET", srv.URL, nil, nil))
			if err != nil {
				t.Fatal(err)
			}
			if resp.Truncated != tt.wantTruncated || resp.SizeBytes != tt.wantSize || int64(len(resp.Body)) > limit {
				t.Errorf("truncated=%v size=%d len=%d", resp.Truncated, resp.SizeBytes, len(resp.Body))
			}
		})
	}
}

func TestDoTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	f := New(Options{Timeout: 200 * time.Millisecond, MaxBodyBytes: 1024, AllowPrivateNetworks: true})
	_, err := f.Do(context.Background(), outbound(t, "GET", srv.URL, nil, nil))
	if err == nil {
		t.Fatal("expected timeout")
	}
	var te interface{ Timeout() bool }
	if !errors.As(err, &te) || !te.Timeout() {
		t.Errorf("err = %v, want timeout", err)
	}
}

func TestDoConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close()
	_, err := allowAll(1024).Do(context.Background(), outbound(t, "GET", addr, nil, nil))
	if err == nil || errors.Is(err, artifact.ErrDestinationNotAllowed) {
		t.Fatalf("err = %v", err)
	}
}

func TestDoBlocksNonPublicDestinations(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()
	p := strconv.Itoa(srv.Listener.Addr().(*net.TCPAddr).Port)

	f := New(Options{Timeout: 2 * time.Second, MaxBodyBytes: 1024})
	for _, target := range []string{
		srv.URL,                   // 127.0.0.1
		"http://localhost:" + p,   // resolves to loopback
		"http://[::1]:" + p,       // IPv6 loopback
		"http://0.0.0.0:" + p,     // unspecified
		"http://169.254.169.254/", // cloud metadata
		"http://10.0.0.1/",        // RFC 1918
		"http://[::ffff:127.0.0.1]:" + p,
	} {
		_, err := f.Do(context.Background(), outbound(t, "GET", target, nil, nil))
		if !errors.Is(err, artifact.ErrDestinationNotAllowed) {
			t.Errorf("%s: err = %v, want ErrDestinationNotAllowed", target, err)
		}
	}
	if hit {
		t.Error("blocked request reached the server")
	}
}

func TestIsPublicAddr(t *testing.T) {
	public := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700:4700::1111", "2001:4860:4860::8888", "::ffff:8.8.8.8"}
	nonPublic := []string{
		"127.0.0.1", "127.255.255.254", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.1",
		"169.254.169.254", "0.0.0.0", "0.1.2.3", "100.64.0.1", "100.127.255.255", "192.0.0.8", "192.0.2.1",
		"198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "239.255.255.250", "240.0.0.1", "255.255.255.255",
		"::", "::1", "fe80::1", "fc00::1", "fd12:3456::1", "ff02::1", "::ffff:10.0.0.1", "::ffff:127.0.0.1",
		"64:ff9b::a00:1", "2001:db8::1", "2002:a00:1::1", "100::1",
	}
	for _, s := range public {
		if !IsPublicAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s should be public", s)
		}
	}
	for _, s := range nonPublic {
		if IsPublicAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s should not be public", s)
		}
	}
	if IsPublicAddr(netip.Addr{}) {
		t.Error("zero Addr should not be public")
	}
	// Boundaries just outside blocked ranges.
	for _, s := range []string{"100.63.255.255", "100.128.0.0", "172.15.255.255", "172.32.0.0", "198.17.255.255", "198.20.0.0"} {
		if !IsPublicAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s should be public", s)
		}
	}
}

func TestDoRawResponseHead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Zeta", "last")
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Add("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
		w.Header().Set("Date", "Mon, 27 Oct 2026 12:00:00 GMT")
		w.WriteHeader(http.StatusCreated)
		if r.URL.Path == "/chunked" {
			w.(http.Flusher).Flush() // forces chunked transfer encoding
		}
		io.WriteString(w, "hello")
	}))
	defer srv.Close()

	resp, err := allowAll(1024).Do(context.Background(), outbound(t, "GET", srv.URL, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	want := "HTTP/1.1 201 Created\r\n" +
		"Content-Length: 5\r\n" +
		"Content-Type: text/plain\r\n" +
		"Date: Mon, 27 Oct 2026 12:00:00 GMT\r\n" +
		"Set-Cookie: a=1\r\n" +
		"Set-Cookie: b=2\r\n" +
		"X-Zeta: last\r\n" +
		"\r\n"
	if string(resp.Head) != want {
		t.Errorf("head =\n%q\nwant\n%q", resp.Head, want)
	}
	if string(resp.Body) != "hello" {
		t.Errorf("body = %q", resp.Body)
	}

	resp, err = allowAll(1024).Do(context.Background(), outbound(t, "GET", srv.URL+"/chunked", nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resp.Head), "\r\nTransfer-Encoding: chunked\r\n") || string(resp.Body) != "hello" {
		t.Errorf("chunked head = %q body = %q", resp.Head, resp.Body)
	}
}

func TestDoIgnoresTLSCertificateErrors(t *testing.T) {
	// httptest serves a self-signed certificate for 127.0.0.1/example.com,
	// so this covers an untrusted issuer and, via the Host override, a
	// hostname mismatch.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "secure hello")
	}))
	defer srv.Close()
	f := allowAll(1024)

	for _, h := range []http.Header{nil, {"Host": {"mismatch.invalid"}}} {
		resp, err := f.Do(context.Background(), outbound(t, "GET", srv.URL, h, nil))
		if err != nil || resp.StatusCode != 200 || string(resp.Body) != "secure hello" {
			t.Fatalf("headers %v: resp = %+v err = %v", h, resp, err)
		}
	}
}

func TestDoAcceptsLegacyTLS(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "legacy")
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS10}
	srv.StartTLS()
	defer srv.Close()

	resp, err := allowAll(1024).Do(context.Background(), outbound(t, "GET", srv.URL, nil, nil))
	if err != nil || string(resp.Body) != "legacy" {
		t.Fatalf("TLS 1.0 server: resp = %+v err = %v", resp, err)
	}
}

func TestDoTLSKeepsAddressPolicy(t *testing.T) {
	hit := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()
	f := New(Options{Timeout: 2 * time.Second, MaxBodyBytes: 1024})
	if _, err := f.Do(context.Background(), outbound(t, "GET", srv.URL, nil, nil)); !errors.Is(err, artifact.ErrDestinationNotAllowed) {
		t.Fatalf("err = %v, want ErrDestinationNotAllowed", err)
	}
	if hit {
		t.Error("blocked request reached the server")
	}
}
