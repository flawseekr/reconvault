// Package fetcher executes outbound HTTP requests with SSRF protection and
// bounded resource usage.
package fetcher

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"

	"github.com/reconvault/reconvault-api/internal/artifact"
)

// Options configure a Fetcher.
type Options struct {
	// Timeout bounds the whole exchange including reading the body.
	Timeout time.Duration
	// MaxBodyBytes is the maximum number of response body bytes retained.
	MaxBodyBytes int64
	// AllowPrivateNetworks disables the destination address policy.
	AllowPrivateNetworks bool
}

// Fetcher implements artifact.Fetcher.
type Fetcher struct {
	client       *http.Client
	maxBodyBytes int64
}

var _ artifact.Fetcher = (*Fetcher)(nil)

// New returns a Fetcher configured with opts.
func New(opts Options) *Fetcher {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if !opts.AllowPrivateNetworks {
		// Control runs after DNS resolution for every connection attempt, so
		// the check applies to the actual peer address and cannot be bypassed
		// with DNS rebinding or hostnames that resolve to internal addresses.
		dialer.Control = denyNonPublic
	}

	transport := &http.Transport{
		// Never route through environment-configured proxies: the address
		// policy must apply to the real destination.
		Proxy:             nil,
		DialContext:       dialer.DialContext,
		ForceAttemptHTTP2: true,
		// TLS certificate errors are always ignored (a project requirement):
		// the module records whatever a target serves, including
		// self-signed, expired, untrusted or hostname-mismatched
		// certificates and legacy TLS versions. The address policy above
		// still applies to every connection.
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS10,
		},
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  opts.Timeout,
		ExpectContinueTimeout:  time.Second,
		MaxIdleConns:           100,
		MaxIdleConnsPerHost:    10,
		IdleConnTimeout:        90 * time.Second,
		MaxResponseHeaderBytes: 1 << 20,
		// Send exactly the headers the caller specified (plus defaults) and
		// record the body exactly as the server sent it.
		DisableCompression: true,
	}

	return &Fetcher{
		client: &http.Client{
			Transport: transport,
			Timeout:   opts.Timeout,
			// Record the response the target actually returned instead of
			// following redirects, which could also lead to unvetted hosts.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		maxBodyBytes: opts.MaxBodyBytes,
	}
}

// Do sends req and returns the response with a size-bounded body.
func (f *Fetcher) Do(ctx context.Context, req artifact.OutboundRequest) (*artifact.Response, error) {
	var body io.Reader
	if req.Body != nil {
		body = strings.NewReader(*req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	for name, values := range req.Headers {
		if name == "Host" {
			httpReq.Host = values[0]
			continue
		}
		httpReq.Header[name] = values
	}

	resp, err := f.client.Do(httpReq)
	if err != nil {
		if errors.Is(err, errBlocked) {
			return nil, artifact.ErrDestinationNotAllowed
		}
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	out := &artifact.Response{
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Head:        responseHead(resp),
		Body:        data,
		SizeBytes:   int64(len(data)),
	}
	if int64(len(data)) > f.maxBodyBytes {
		out.Body = data[:f.maxBodyBytes]
		out.Truncated = true
		out.SizeBytes = f.maxBodyBytes
		if resp.ContentLength > f.maxBodyBytes {
			out.SizeBytes = resp.ContentLength
		}
	}
	return out, nil
}

// responseHead renders the status line and headers in HTTP/1.x wire
// format. net/http does not expose the original bytes: header names are
// canonicalized and written in sorted order, and Transfer-Encoding, which
// net/http moves out of Header, is restored.
func responseHead(resp *http.Response) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s %s\r\n", resp.Proto, resp.Status)
	h := resp.Header
	if len(resp.TransferEncoding) > 0 && h.Get("Transfer-Encoding") == "" {
		h = h.Clone()
		h.Set("Transfer-Encoding", strings.Join(resp.TransferEncoding, ", "))
	}
	h.Write(&b) // sorted by name; CR/LF in values replaced with spaces
	b.WriteString("\r\n")
	return b.Bytes()
}

var errBlocked = errors.New("destination address is not allowed")

// denyNonPublic is a net.Dialer Control hook that rejects connections to
// non-public addresses.
func denyNonPublic(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return errBlocked
	}
	if !IsPublicAddr(ap.Addr()) {
		return errBlocked
	}
	return nil
}

// nonPublicPrefixes lists special-purpose ranges (RFC 6890 and successors)
// that are not covered by the netip.Addr predicates used in IsPublicAddr.
var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this" network
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, incl. broadcast
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64 (may embed private IPv4)
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local-use NAT64
	netip.MustParsePrefix("100::/64"),        // discard-only
	netip.MustParsePrefix("2001::/23"),       // IETF protocol assignments
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("2002::/16"),       // 6to4 (may embed private IPv4)
}

// IsPublicAddr reports whether addr is a globally routable unicast address.
func IsPublicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, p := range nonPublicPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}
