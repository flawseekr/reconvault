package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// errorBody is the JSON envelope for every error response.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Field      string `json:"field,omitempty"`
	ArtifactID string `json:"artifact_id,omitempty"`
}

// Error codes returned in errorDetail.Code.
const (
	codeBadRequest            = "bad_request"
	codeValidation            = "validation_error"
	codeUnauthorized          = "unauthorized"
	codeDestinationNotAllowed = "destination_not_allowed"
	codeNotFound              = "not_found"
	codeMethodNotAllowed      = "method_not_allowed"
	codePayloadTooLarge       = "payload_too_large"
	codeUnsupportedMediaType  = "unsupported_media_type"
	codeInternal              = "internal_error"
	codeUpstreamFailed        = "upstream_request_failed"
	codeUpstreamTimeout       = "upstream_timeout"
	codeUnavailable           = "service_unavailable"
)

func writeJSON(w http.ResponseWriter, log *slog.Logger, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Warn("write response", "error", err)
	}
}

func writeError(w http.ResponseWriter, log *slog.Logger, status int, d errorDetail) {
	writeJSON(w, log, status, errorBody{Error: d})
}
