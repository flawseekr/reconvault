package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"github.com/reconvault/reconvault-api/api"
	"github.com/reconvault/reconvault-api/internal/artifact"
	"github.com/reconvault/reconvault-api/internal/recon"
)

// ArtifactService is the application logic the handlers depend on.
type ArtifactService interface {
	Execute(ctx context.Context, in artifact.ExecuteInput) (*artifact.ExecuteResult, error)
	Get(ctx context.Context, id string) (*artifact.Artifact, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, p artifact.ListParams) (*artifact.ListPage, error)
	Search(ctx context.Context, p artifact.SearchParams) ([]artifact.SearchResult, error)
	Domains(ctx context.Context, p artifact.DomainParams) ([]artifact.Domain, error)
}

// Pinger checks dependency readiness.
type Pinger interface {
	Ping(ctx context.Context) error
}

type handlers struct {
	svc             ArtifactService
	recon           recon.Reader
	db              Pinger
	log             *slog.Logger
	maxRequestBytes int64
}

// executeRequest is the body of POST /api/v1/http/request.
type executeRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    *string           `json:"body"`
}

// executeResponse is the stored artifact plus execution metadata.
type executeResponse struct {
	*artifact.Artifact
	ResponseBodyTruncated bool `json:"response_body_truncated"`
}

type listResponse[T any] struct {
	Data []T `json:"data"`
	Meta any `json:"meta"`
}

type listMeta struct {
	SortBy    string             `json:"sort_by"`
	SortOrder artifact.SortOrder `json:"sort_order"`
	Hostname  *string            `json:"hostname"`
	Limit     int                `json:"limit"`
	// NextCursor is passed as ?cursor= to fetch the next page; null on the
	// last page.
	NextCursor *string `json:"next_cursor"`
}

type searchMeta struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type domainMeta struct {
	SortBy    string             `json:"sort_by"`
	SortOrder artifact.SortOrder `json:"sort_order"`
	Limit     int                `json:"limit"`
}

func (h *handlers) executeRequest(w http.ResponseWriter, r *http.Request) {
	if !isJSON(r.Header.Get("Content-Type")) {
		writeError(w, h.log, http.StatusUnsupportedMediaType,
			errorDetail{Code: codeUnsupportedMediaType, Message: "Content-Type must be application/json"})
		return
	}

	var req executeRequest
	if err := decodeJSON(w, r, h.maxRequestBytes, &req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, h.log, http.StatusRequestEntityTooLarge, errorDetail{
				Code: codePayloadTooLarge, Message: fmt.Sprintf("request body must not exceed %d bytes", tooLarge.Limit),
			})
			return
		}
		writeError(w, h.log, http.StatusBadRequest, errorDetail{Code: codeBadRequest, Message: err.Error()})
		return
	}

	res, err := h.svc.Execute(r.Context(), artifact.ExecuteInput{
		Method: req.Method, URL: req.URL, Headers: req.Headers, Body: req.Body,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/http/artifacts/"+res.Artifact.ID)
	writeJSON(w, h.log, http.StatusCreated, executeResponse{Artifact: res.Artifact, ResponseBodyTruncated: res.Truncated})
}

func (h *handlers) listArtifacts(w http.ResponseWriter, r *http.Request) {
	p, err := parseListParams(r.URL.Query())
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	page, err := h.svc.List(r.Context(), p)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	meta := listMeta{SortBy: p.SortBy, SortOrder: p.SortOrder, Limit: p.Limit}
	if p.Hostname != "" {
		meta.Hostname = &p.Hostname
	}
	if page.Next != nil {
		meta.NextCursor = ptr(encodeCursor(page.Next))
	}
	writeJSON(w, h.log, http.StatusOK, listResponse[artifact.Summary]{Data: nonNil(page.Items), Meta: meta})
}

func (h *handlers) deleteArtifact(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), r.PathValue("id")); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) getArtifact(w http.ResponseWriter, r *http.Request) {
	a, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, a)
}

func (h *handlers) searchArtifacts(w http.ResponseWriter, r *http.Request) {
	p, err := parseSearchParams(r.URL.Query())
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	items, err := h.svc.Search(r.Context(), p)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, listResponse[artifact.SearchResult]{
		Data: nonNil(items), Meta: searchMeta{Query: p.Query, Limit: p.Limit},
	})
}

func (h *handlers) listDomains(w http.ResponseWriter, r *http.Request) {
	p, err := parseDomainParams(r.URL.Query())
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	items, err := h.svc.Domains(r.Context(), p)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, listResponse[artifact.Domain]{
		Data: nonNil(items), Meta: domainMeta{SortBy: p.SortBy, SortOrder: p.SortOrder, Limit: p.Limit},
	})
}

func (h *handlers) openAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(api.Spec)
}

func (h *handlers) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, h.log, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handlers) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.db.Ping(ctx); err != nil {
		h.log.Warn("readiness check failed", "error", err)
		writeError(w, h.log, http.StatusServiceUnavailable,
			errorDetail{Code: codeUnavailable, Message: "database unavailable"})
		return
	}
	writeJSON(w, h.log, http.StatusOK, map[string]string{"status": "ok"})
}

// writeServiceError maps domain errors to HTTP responses. Unexpected errors
// are logged and reported generically so internals are not leaked.
func (h *handlers) writeServiceError(w http.ResponseWriter, err error) {
	var (
		pe *paramError
		ve *artifact.ValidationError
		ue *artifact.UpstreamError
	)
	switch {
	case errors.As(err, &pe):
		writeError(w, h.log, http.StatusBadRequest, errorDetail{Code: codeValidation, Message: pe.Error(), Field: pe.field})
	case errors.As(err, &ve):
		writeError(w, h.log, http.StatusBadRequest, errorDetail{Code: codeValidation, Message: ve.Error(), Field: ve.Field})
	case errors.Is(err, artifact.ErrNotFound):
		writeError(w, h.log, http.StatusNotFound, errorDetail{Code: codeNotFound, Message: "artifact not found"})
	case errors.Is(err, recon.ErrNotFound):
		writeError(w, h.log, http.StatusNotFound, errorDetail{Code: codeNotFound, Message: "domain not found"})
	case errors.Is(err, artifact.ErrDestinationNotAllowed):
		writeError(w, h.log, http.StatusForbidden, errorDetail{
			Code: codeDestinationNotAllowed, Message: "the target resolves to a non-public address, which is not allowed",
		})
	case errors.As(err, &ue):
		// The transport error is logged but not returned: it can reveal
		// details about the server's network environment.
		h.log.Warn("upstream request failed", "artifact_id", ue.ArtifactID, "error", ue.Err)
		if ue.Timeout {
			writeError(w, h.log, http.StatusGatewayTimeout, errorDetail{
				Code: codeUpstreamTimeout, Message: "the upstream request timed out", ArtifactID: ue.ArtifactID,
			})
			return
		}
		writeError(w, h.log, http.StatusBadGateway, errorDetail{
			Code: codeUpstreamFailed, Message: "the upstream request failed", ArtifactID: ue.ArtifactID,
		})
	default:
		h.log.Error("request failed", "error", err)
		writeError(w, h.log, http.StatusInternalServerError, errorDetail{Code: codeInternal, Message: "internal server error"})
	}
}

// decodeJSON strictly decodes a single JSON object from the request body.
func decodeJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var (
			tooLarge  *http.MaxBytesError
			syntaxErr *json.SyntaxError
			typeErr   *json.UnmarshalTypeError
		)
		switch {
		case errors.As(err, &tooLarge):
			return err
		case errors.Is(err, io.EOF):
			return errors.New("request body must not be empty")
		case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
			return errors.New("request body contains malformed JSON")
		case errors.As(err, &typeErr):
			if typeErr.Field != "" {
				return fmt.Errorf("field %q has an invalid type", typeErr.Field)
			}
			return errors.New("request body must be a JSON object")
		default:
			// Unknown fields and similar decoder errors are safe to surface.
			return fmt.Errorf("invalid request body: %v", err)
		}
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return err
		}
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func isJSON(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && mt == "application/json"
}

func ptr[T any](v T) *T { return &v }

// nonNil makes empty result sets encode as [] rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
