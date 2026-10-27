package httpapi

import (
	"net/http"

	"github.com/reconvault/reconvault-api/internal/recon"
)

type dnsRecordsMeta struct {
	Hostname string `json:"hostname"`
	Total    int    `json:"total"`
}

type subdomainsMeta struct {
	RootDomain string `json:"root_domain"`
	Total      int    `json:"total"`
}

type rootDomainsMeta struct {
	Limit int `json:"limit"`
}

func (h *handlers) getDomain(w http.ResponseWriter, r *http.Request) {
	host, err := parseHostname("hostname", r.PathValue("hostname"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	d, err := h.recon.Domain(r.Context(), host)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, d)
}

// listDNSRecords returns a hostname's complete DNS history (no limit),
// newest first. An unknown hostname yields an empty list.
func (h *handlers) listDNSRecords(w http.ResponseWriter, r *http.Request) {
	host, err := parseHostname("hostname", r.PathValue("hostname"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	records, err := h.recon.DNSHistory(r.Context(), host)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, listResponse[recon.DNSRecord]{
		Data: nonNil(records), Meta: dnsRecordsMeta{Hostname: host, Total: len(records)},
	})
}

// listSubdomains returns the distinct hostnames under a root domain.
func (h *handlers) listSubdomains(w http.ResponseWriter, r *http.Request) {
	root, err := parseHostname("root_domain", r.PathValue("root_domain"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	subs, err := h.recon.Subdomains(r.Context(), root)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, listResponse[recon.Subdomain]{
		Data: nonNil(subs), Meta: subdomainsMeta{RootDomain: root, Total: len(subs)},
	})
}

// listRootDomains returns root domains with their distinct hostname counts.
func (h *handlers) listRootDomains(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query(), recon.DefaultRootDomainLimit, recon.MaxRootDomainLimit)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	roots, err := h.recon.RootDomains(r.Context(), limit)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, listResponse[recon.RootDomain]{
		Data: nonNil(roots), Meta: rootDomainsMeta{Limit: limit},
	})
}
