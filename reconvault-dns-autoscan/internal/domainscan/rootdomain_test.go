package domainscan

import "testing"

func TestRootDomain(t *testing.T) {
	tests := map[string]string{
		"www.example.com":         "example.com",
		"example.com":             "example.com",
		"WWW.Example.COM.":        "example.com",
		"a.b.example.co.uk":       "example.co.uk",
		"example.co.uk":           "example.co.uk",
		"co.uk":                   "co.uk",
		"api.service.gov.uk":      "service.gov.uk",
		"shop.example.com.au":     "example.com.au",
		"x.y.example.co.jp":       "example.co.jp",
		"user.github.io":          "github.io",
		"bucket.s3.amazonaws.com": "amazonaws.com",
		"foo.blogspot.co.uk":      "blogspot.co.uk",
		"a.b.invalid":             "b.invalid",
		"no-such-host.invalid":    "no-such-host.invalid",
		"localhost":               "localhost",
		"192.0.2.1":               "192.0.2.1",
		"2001:db8::1":             "2001:db8::1",
		"xn--80ak6aa92e.com":      "xn--80ak6aa92e.com",
		"":                        "",
	}
	for in, want := range tests {
		if got := RootDomain(in); got != want {
			t.Errorf("RootDomain(%q) = %q, want %q", in, got, want)
		}
	}
}
