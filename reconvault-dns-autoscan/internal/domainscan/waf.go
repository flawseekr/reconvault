package domainscan

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/projectdiscovery/cdncheck"

	"github.com/reconvault/reconvault-dns-autoscan/internal/netguard"
)

// WAFResult reports whether a WAF was detected.
type WAFResult struct {
	// Detected is nil when the checks were inconclusive.
	Detected *bool
	// Provider names the detected WAF, when known.
	Provider string
	// Evidence explains the detection, for logs.
	Evidence string
}

// wafCNAMESuffixes are CNAME targets that only exist when traffic passes
// through a WAF product. CDN-only targets (CloudFront, Fastly, Akamai edge
// hostnames, ...) are deliberately absent: a CDN does not imply a WAF.
var wafCNAMESuffixes = map[string]string{
	"incapdns.net":       "Imperva Incapsula",
	"impervadns.net":     "Imperva",
	"cdn.cloudflare.net": "Cloudflare",
	"sucuri.net":         "Sucuri",
}

// signature is a passive HTTP fingerprint, modelled on wafw00f's passive
// checks: response headers and cookies that WAF products add to every
// response they proxy. Headers that only identify a CDN or cache (Akamai's
// AkamaiGHost, Azure Front Door's X-Azure-Ref, NetScaler's cache Via) are
// excluded: they do not prove a WAF is in the path.
type signature struct {
	provider string
	match    func(h http.Header, cookies []string) bool
}

func header(name, contains string) func(http.Header, []string) bool {
	return func(h http.Header, _ []string) bool {
		v := h.Values(name)
		if contains == "" {
			return len(v) > 0
		}
		for _, x := range v {
			if strings.Contains(strings.ToLower(x), contains) {
				return true
			}
		}
		return false
	}
}

func cookie(prefix string) func(http.Header, []string) bool {
	return func(_ http.Header, cookies []string) bool {
		for _, c := range cookies {
			if strings.HasPrefix(strings.ToLower(c), prefix) {
				return true
			}
		}
		return false
	}
}

var signatures = []signature{
	{"Cloudflare", header("Cf-Ray", "")},
	{"Cloudflare", header("Server", "cloudflare")},
	{"Imperva Incapsula", header("X-Iinfo", "")},
	{"Imperva Incapsula", header("X-Cdn", "incapsula")},
	{"Imperva Incapsula", cookie("incap_ses_")},
	{"Imperva Incapsula", cookie("visid_incap_")},
	{"Sucuri", header("X-Sucuri-Id", "")},
	{"Sucuri", header("Server", "sucuri")},
	{"AWS WAF", header("X-Amzn-Waf-Action", "")},
	{"AWS WAF", cookie("aws-waf-token")},
	{"F5 BIG-IP ASM", cookie("ts01")},
	{"F5 BIG-IP ASM", header("X-Waf-Status", "")},
	{"Barracuda", cookie("barra_counter_session")},
	{"Fortinet FortiWeb", cookie("fortiwafsid")},
	{"Citrix NetScaler AppFirewall", cookie("citrix_ns_id")},
	{"ModSecurity", header("Server", "mod_security")},
	{"StackPath", header("X-Sp-Waf", "")},
	{"Wallarm", header("Server", "wallarm")},
	{"Reblaze", cookie("rbzid")},
	{"DDoS-Guard", header("Server", "ddos-guard")},
	{"Qrator", header("Server", "qrator")},
	{"ArvanCloud", header("Server", "arvancloud")},
}

// WAFDetector combines provider IP ranges, CNAME targets and passive HTTP
// fingerprints.
type WAFDetector struct {
	ranges *cdncheck.Client
	http   *http.Client
}

// NewWAFDetector returns a detector. The HTTP probe ignores TLS
// certificate errors (project policy) and enforces the address policy.
func NewWAFDetector(timeout time.Duration, policy netguard.Policy) *WAFDetector {
	dialer := &net.Dialer{Timeout: timeout, Control: policy.DialControl}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS10},
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	return &WAFDetector{
		ranges: cdncheck.New(),
		http: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Detect checks host, its resolved addresses and CNAME chain.
func (d *WAFDetector) Detect(ctx context.Context, host string, addrs []netip.Addr, cnames []string) WAFResult {
	for _, addr := range addrs {
		if ok, provider, err := d.ranges.CheckWAF(net.IP(addr.AsSlice())); err == nil && ok {
			return detected(provider, "address "+addr.String()+" in provider range")
		}
	}
	for _, cn := range cnames {
		for suffix, provider := range wafCNAMESuffixes {
			if cn == suffix || strings.HasSuffix(cn, "."+suffix) {
				return detected(provider, "CNAME "+cn)
			}
		}
	}

	// Passive HTTP fingerprinting: HTTPS first, then HTTP.
	probed := false
	for _, scheme := range []string{"https", "http"} {
		h, cookies, ok := d.probe(ctx, scheme+"://"+hostForURL(host)+"/")
		if !ok {
			continue
		}
		probed = true
		for _, sig := range signatures {
			if sig.match(h, cookies) {
				return detected(sig.provider, "HTTP fingerprint ("+scheme+")")
			}
		}
	}
	if probed {
		no := false
		return WAFResult{Detected: &no, Evidence: "no WAF signals"}
	}
	return WAFResult{Evidence: "host unreachable over HTTP(S); inconclusive"}
}

func (d *WAFDetector) probe(ctx context.Context, url string) (http.Header, []string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) reconvault-dns-autoscan")
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, nil, false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	var cookies []string
	for _, c := range resp.Cookies() {
		cookies = append(cookies, c.Name)
	}
	return resp.Header, cookies, true
}

func hostForURL(host string) string {
	if addr, err := netip.ParseAddr(host); err == nil && addr.Is6() {
		return "[" + host + "]"
	}
	return host
}

func detected(provider, evidence string) WAFResult {
	yes := true
	return WAFResult{Detected: &yes, Provider: provider, Evidence: evidence}
}
