// Package httpapi exposes the artifact service over HTTP/JSON.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/reconvault/reconvault-api/internal/recon"
)

// Options configure the HTTP handler.
type Options struct {
	APIKey          string
	MaxRequestBytes int64
	// Recon reads the domain and DNS data written by the autoscan service.
	Recon recon.Reader
}

// route is one registered endpoint.
type route struct {
	method, path string
	handler      http.HandlerFunc
	// public routes are served without the API key.
	public bool
}

// routes lists every endpoint. api/swagger.json must document exactly
// these; TestOpenAPISpecMatchesRoutes enforces it.
func (h *handlers) routes() []route {
	return []route{
		{"GET", "/healthz", h.healthz, true},
		{"GET", "/readyz", h.readyz, true},
		{"GET", "/swagger.json", h.openAPISpec, true},
		{"POST", "/api/v1/http/request", h.executeRequest, false},
		{"GET", "/api/v1/http/artifacts", h.listArtifacts, false},
		// More specific than /{id}, so it takes precedence.
		{"GET", "/api/v1/http/artifacts/search", h.searchArtifacts, false},
		{"GET", "/api/v1/http/artifacts/{id}", h.getArtifact, false},
		{"DELETE", "/api/v1/http/artifacts/{id}", h.deleteArtifact, false},
		{"GET", "/api/v1/http/domains", h.listDomains, false},
		{"GET", "/api/v1/domains/{hostname}", h.getDomain, false},
		{"GET", "/api/v1/dns_records/{hostname}", h.listDNSRecords, false},
		{"GET", "/api/v1/subdomains/{root_domain}", h.listSubdomains, false},
		{"GET", "/api/v1/root_domains", h.listRootDomains, false},
	}
}

// NewHandler builds the root HTTP handler with all routes and middleware.
// Health endpoints and the OpenAPI document are unauthenticated; everything
// under /api/v1 requires the API key.
func NewHandler(svc ArtifactService, db Pinger, log *slog.Logger, opts Options) http.Handler {
	h := &handlers{svc: svc, recon: opts.Recon, db: db, log: log, maxRequestBytes: opts.MaxRequestBytes}

	api := http.NewServeMux()
	root := http.NewServeMux()
	for _, rt := range h.routes() {
		mux := api
		if rt.public {
			mux = root
		}
		mux.HandleFunc(rt.method+" "+rt.path, rt.handler)
	}
	root.Handle("/", requireAPIKey(opts.APIKey, log, jsonFallback(api, log)))

	return recoverPanics(log, logRequests(log, securityHeaders(root)))
}

// jsonFallback renders the mux's own "not found" and "method not allowed"
// outcomes as JSON errors, preserving the status code and Allow header.
func jsonFallback(mux *http.ServeMux, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		// No route matched: let the mux decide between 404 and 405
		// without writing its plain-text body to the client.
		cw := &captureWriter{header: http.Header{}}
		h.ServeHTTP(cw, r)
		if cw.status == http.StatusMethodNotAllowed {
			w.Header().Set("Allow", cw.header.Get("Allow"))
			writeError(w, log, http.StatusMethodNotAllowed,
				errorDetail{Code: codeMethodNotAllowed, Message: "method not allowed"})
			return
		}
		writeError(w, log, http.StatusNotFound, errorDetail{Code: codeNotFound, Message: "resource not found"})
	})
}

// captureWriter records a status code and headers and discards the body.
type captureWriter struct {
	header http.Header
	status int
}

func (c *captureWriter) Header() http.Header         { return c.header }
func (c *captureWriter) Write(b []byte) (int, error) { return len(b), nil }
func (c *captureWriter) WriteHeader(status int)      { c.status = status }
