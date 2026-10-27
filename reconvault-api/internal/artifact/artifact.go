// Package artifact contains the domain model and business logic for HTTP
// artifacts: recorded outbound HTTP requests and their responses.
package artifact

import (
	"encoding/json"
	"errors"
	"time"
)

// Artifact is a fully recorded HTTP request/response pair.
type Artifact struct {
	ID                    string          `json:"id"`
	HTTPMethod            string          `json:"http_method"`
	Hostname              string          `json:"hostname"`
	EndpointPath          string          `json:"endpoint_path"`
	FullURL               string          `json:"full_url"`
	RequestedAt           time.Time       `json:"requested_at"`
	ResponseStatusCode    *int            `json:"response_status_code"`
	ResponseContentType   *string         `json:"response_content_type"`
	ResponseBodySizeBytes *int64          `json:"response_body_size_bytes"`
	RequestHeaders        json.RawMessage `json:"request_headers"`
	RequestBody           *string         `json:"request_body"`
	// Response is the raw HTTP response: status line, headers, blank
	// line and body.
	Response *string `json:"response"`
}

// Summary is the projection returned by the list endpoint.
type Summary struct {
	ID                    string    `json:"id"`
	HTTPMethod            string    `json:"http_method"`
	FullURL               string    `json:"full_url"`
	ResponseStatusCode    *int      `json:"response_status_code"`
	ResponseContentType   *string   `json:"response_content_type"`
	ResponseBodySizeBytes *int64    `json:"response_body_size_bytes"`
	RequestedAt           time.Time `json:"requested_at"`
}

// SearchResult is the metadata-only projection returned by search.
type SearchResult struct {
	ID                    string    `json:"id"`
	HTTPMethod            string    `json:"http_method"`
	Hostname              string    `json:"hostname"`
	EndpointPath          string    `json:"endpoint_path"`
	FullURL               string    `json:"full_url"`
	RequestedAt           time.Time `json:"requested_at"`
	ResponseStatusCode    *int      `json:"response_status_code"`
	ResponseContentType   *string   `json:"response_content_type"`
	ResponseBodySizeBytes *int64    `json:"response_body_size_bytes"`
}

// Domain aggregates artifacts per hostname.
type Domain struct {
	Hostname        string    `json:"hostname"`
	ArtifactCount   int64     `json:"artifact_count"`
	LastRequestedAt time.Time `json:"last_requested_at"`
}

// SortOrder is an ORDER BY direction.
type SortOrder string

const (
	SortAsc  SortOrder = "ASC"
	SortDesc SortOrder = "DESC"
)

// ListParams are the validated parameters of an artifact listing.
type ListParams struct {
	SortBy    string // one of ListSortFields
	SortOrder SortOrder
	Hostname  string // empty means no filter
	Limit     int
	// After, when set, starts the page after this cursor. Its SortBy,
	// SortOrder and Hostname must equal the fields above.
	After *ListCursor
}

// SearchParams are the validated parameters of an artifact search.
type SearchParams struct {
	Query string
	Limit int
}

// DomainParams are the validated parameters of a domain listing.
type DomainParams struct {
	SortBy    string // one of DomainSortFields
	SortOrder SortOrder
	Limit     int
}

// Sort fields accepted by the list endpoints.
const (
	DomainSortTotalArtifacts = "total_artifacts"
	DomainSortLatestUpdate   = "latest_update"
)

// ListSortFields are the sortable columns of GET /api/v1/http/artifacts.
var ListSortFields = []string{
	"http_method",
	"full_url",
	"response_status_code",
	"response_content_type",
	"response_body_size_bytes",
	"requested_at",
}

// DomainSortFields are the sort keys of GET /api/v1/http/domains.
var DomainSortFields = []string{DomainSortTotalArtifacts, DomainSortLatestUpdate}

// Errors returned by the service. Handlers map them to HTTP status codes.
var (
	ErrNotFound = errors.New("artifact not found")
	// ErrDestinationNotAllowed is returned when the target resolves to an
	// address blocked by the SSRF policy.
	ErrDestinationNotAllowed = errors.New("destination address is not allowed")
)

// ValidationError describes invalid client input.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

func invalid(field, msg string) error {
	return &ValidationError{Field: field, Message: msg}
}

// UpstreamError reports that the outbound request failed after it was
// attempted. The attempt is still recorded as an artifact.
type UpstreamError struct {
	ArtifactID string
	Timeout    bool
	Err        error
}

func (e *UpstreamError) Error() string { return "upstream request failed: " + e.Err.Error() }
func (e *UpstreamError) Unwrap() error { return e.Err }
