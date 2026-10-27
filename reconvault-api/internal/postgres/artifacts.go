// Package postgres implements artifact persistence on PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reconvault/reconvault-api/internal/artifact"
)

// ArtifactRepository implements artifact.Repository.
type ArtifactRepository struct {
	pool *pgxpool.Pool
}

var _ artifact.Repository = (*ArtifactRepository)(nil)

// NewArtifactRepository returns a repository backed by pool.
func NewArtifactRepository(pool *pgxpool.Pool) *ArtifactRepository {
	return &ArtifactRepository{pool: pool}
}

// listSortColumns whitelists the ORDER BY columns of List. Identifiers are
// never taken from user input directly.
var listSortColumns = map[string]string{
	"http_method":              "http_method",
	"full_url":                 "full_url",
	"response_status_code":     "response_status_code",
	"response_content_type":    "response_content_type",
	"response_body_size_bytes": "response_body_size_bytes",
	"requested_at":             "requested_at",
}

// domainSortColumns whitelists the ORDER BY expressions of Domains.
var domainSortColumns = map[string]string{
	artifact.DomainSortTotalArtifacts: "artifact_count",
	artifact.DomainSortLatestUpdate:   "last_requested_at",
}

func sortDirection(o artifact.SortOrder) (string, error) {
	switch o {
	case artifact.SortAsc:
		return "ASC", nil
	case artifact.SortDesc:
		return "DESC", nil
	default:
		return "", fmt.Errorf("invalid sort order %q", o)
	}
}

// Create inserts a and populates its generated ID.
func (r *ArtifactRepository) Create(ctx context.Context, a *artifact.Artifact) error {
	const q = `
		INSERT INTO http_artifacts (
			http_method, hostname, endpoint_path, full_url, requested_at,
			response_status_code, response_content_type, response_body_size_bytes,
			request_headers, request_body, response
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id`

	var headers any
	if a.RequestHeaders != nil {
		headers = string(a.RequestHeaders)
	}
	err := r.pool.QueryRow(ctx, q,
		a.HTTPMethod, a.Hostname, a.EndpointPath, a.FullURL, a.RequestedAt,
		a.ResponseStatusCode, a.ResponseContentType, a.ResponseBodySizeBytes,
		headers, a.RequestBody, a.Response,
	).Scan(&a.ID)
	if err != nil {
		return fmt.Errorf("insert artifact: %w", err)
	}
	return nil
}

// Get returns the artifact with the given ID or artifact.ErrNotFound.
func (r *ArtifactRepository) Get(ctx context.Context, id string) (*artifact.Artifact, error) {
	const q = `
		SELECT id, http_method, hostname, endpoint_path, full_url, requested_at,
		       response_status_code, response_content_type, response_body_size_bytes,
		       request_headers, request_body, response
		FROM http_artifacts
		WHERE id = $1`

	var (
		a       artifact.Artifact
		headers []byte
	)
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&a.ID, &a.HTTPMethod, &a.Hostname, &a.EndpointPath, &a.FullURL, &a.RequestedAt,
		&a.ResponseStatusCode, &a.ResponseContentType, &a.ResponseBodySizeBytes,
		&headers, &a.RequestBody, &a.Response,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, artifact.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get artifact: %w", err)
	}
	if headers != nil {
		a.RequestHeaders = headers
	}
	a.RequestedAt = a.RequestedAt.UTC()
	return &a, nil
}

// List returns artifact summaries ordered by the requested column. id is
// used as a tie-breaker so ordering is deterministic.
func (r *ArtifactRepository) List(ctx context.Context, p artifact.ListParams) ([]artifact.Summary, error) {
	col, ok := listSortColumns[p.SortBy]
	if !ok {
		return nil, fmt.Errorf("invalid sort field %q", p.SortBy)
	}
	dir, err := sortDirection(p.SortOrder)
	if err != nil {
		return nil, err
	}

	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	var hostCond string
	if p.Hostname != "" {
		hostCond = "hostname = " + arg(p.Hostname)
	}
	// Without a cursor the page is a single segment with no keyset bound.
	segments := []string{""}
	if p.After != nil {
		if segments, err = keysetSegments(col, p.SortOrder, p.After, arg); err != nil {
			return nil, err
		}
	}
	limit := arg(p.Limit)
	orderBy := fmt.Sprintf("ORDER BY %s %s, id %s LIMIT %s", col, dir, dir, limit)

	selects := make([]string, len(segments))
	for i, seg := range segments {
		var conds []string
		for _, c := range []string{hostCond, seg} {
			if c != "" {
				conds = append(conds, c)
			}
		}
		q := "SELECT " + summaryColumns + " FROM http_artifacts"
		if len(conds) > 0 {
			q += " WHERE " + strings.Join(conds, " AND ")
		}
		selects[i] = q + " " + orderBy
	}
	query := selects[0]
	if len(selects) > 1 {
		// Each segment is an index range scan bounded by LIMIT; the outer
		// sort only orders the at most 2*limit rows they return.
		query = "SELECT * FROM ((" + strings.Join(selects, ") UNION ALL (") + ")) page " + orderBy
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (artifact.Summary, error) {
		var s artifact.Summary
		err := row.Scan(&s.ID, &s.HTTPMethod, &s.FullURL, &s.ResponseStatusCode,
			&s.ResponseContentType, &s.ResponseBodySizeBytes, &s.RequestedAt)
		s.RequestedAt = s.RequestedAt.UTC()
		return s, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan artifacts: %w", err)
	}
	return out, nil
}

const summaryColumns = `id, http_method, full_url, response_status_code, response_content_type,
	response_body_size_bytes, requested_at`

// keysetSegments returns the conditions selecting the rows that sort
// strictly after the cursor in ORDER BY col <dir>, id <dir>. PostgreSQL
// sorts NULLs last for ASC and first for DESC, and the row comparison
// (col, id) > (v, id) is never true when col is NULL.
//
// When the remaining rows span both a non-NULL range and the NULL group,
// two segments are returned instead of one OR-ed condition: an OR cannot
// bound an index scan, which would turn deep pages into scans from the
// start of the index. Each segment is index-bounded on its own.
func keysetSegments(col string, order artifact.SortOrder, c *artifact.ListCursor, arg func(any) string) ([]string, error) {
	v, err := artifact.ParseSortValue(c.SortBy, c.Value)
	if err != nil {
		return nil, err
	}
	id := arg(c.ID)
	nullable := artifact.SortFieldNullable(c.SortBy)
	switch {
	case order == artifact.SortAsc && v != nil && nullable:
		// Remaining non-NULL rows, then the trailing NULL group.
		return []string{
			fmt.Sprintf("(%s, id) > (%s, %s)", col, arg(v), id),
			col + " IS NULL",
		}, nil
	case order == artifact.SortAsc && v != nil:
		return []string{fmt.Sprintf("(%s, id) > (%s, %s)", col, arg(v), id)}, nil
	case order == artifact.SortAsc:
		// Inside the trailing NULL group.
		return []string{fmt.Sprintf("%s IS NULL AND id > %s", col, id)}, nil
	case v != nil:
		// DESC past the leading NULL group.
		return []string{fmt.Sprintf("(%s, id) < (%s, %s)", col, arg(v), id)}, nil
	default:
		// DESC inside the leading NULL group: its remainder, then all
		// non-NULL rows.
		return []string{
			fmt.Sprintf("%s IS NULL AND id < %s", col, id),
			col + " IS NOT NULL",
		}, nil
	}
}

// Delete permanently removes the artifact with the given ID.
func (r *ArtifactRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM http_artifacts WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return artifact.ErrNotFound
	}
	return nil
}

// searchQuery matches the escaped ILIKE pattern against the search document
// of every row. The function call must be identical to the expression of
// idx_http_artifacts_search_trgm (migration 0003) for the index to be used.
const searchQuery = `
	SELECT id, http_method, hostname, endpoint_path, full_url, requested_at,
	       response_status_code, response_content_type, response_body_size_bytes
	FROM http_artifacts
	WHERE http_artifact_search_document(
	          id, http_method, hostname, endpoint_path, full_url, requested_at,
	          response_status_code, response_content_type, response_body_size_bytes,
	          request_headers, request_body, response
	      ) ILIKE $1
	ORDER BY requested_at DESC, id DESC
	LIMIT $2`

// Search performs a case-insensitive substring search across all fields.
func (r *ArtifactRepository) Search(ctx context.Context, p artifact.SearchParams) ([]artifact.SearchResult, error) {
	pattern := "%" + escapeLike(p.Query) + "%"
	// QueryExecModeExec uses an unnamed statement, which PostgreSQL always
	// plans with the actual pattern. A cached generic plan cannot know the
	// term's selectivity: it may bitmap-scan the trigram index for a term
	// present in every row (measured 2.6 s vs 0.6 ms at 200k rows) instead
	// of walking requested_at and stopping at LIMIT.
	rows, err := r.pool.Query(ctx, searchQuery, pgx.QueryExecModeExec, pattern, p.Limit)
	if err != nil {
		return nil, fmt.Errorf("search artifacts: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (artifact.SearchResult, error) {
		var s artifact.SearchResult
		err := row.Scan(&s.ID, &s.HTTPMethod, &s.Hostname, &s.EndpointPath, &s.FullURL,
			&s.RequestedAt, &s.ResponseStatusCode, &s.ResponseContentType, &s.ResponseBodySizeBytes)
		s.RequestedAt = s.RequestedAt.UTC()
		return s, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan search results: %w", err)
	}
	return out, nil
}

// Domains aggregates artifacts per hostname. hostname is the final
// tie-breaker so ordering is deterministic.
func (r *ArtifactRepository) Domains(ctx context.Context, p artifact.DomainParams) ([]artifact.Domain, error) {
	col, ok := domainSortColumns[p.SortBy]
	if !ok {
		return nil, fmt.Errorf("invalid sort field %q", p.SortBy)
	}
	dir, err := sortDirection(p.SortOrder)
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf(`
		SELECT hostname,
		       COUNT(*)          AS artifact_count,
		       MAX(requested_at) AS last_requested_at
		FROM http_artifacts
		GROUP BY hostname
		ORDER BY %s %s, hostname ASC
		LIMIT $1`, col, dir)

	rows, err := r.pool.Query(ctx, q, p.Limit)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (artifact.Domain, error) {
		var d artifact.Domain
		err := row.Scan(&d.Hostname, &d.ArtifactCount, &d.LastRequestedAt)
		d.LastRequestedAt = d.LastRequestedAt.UTC()
		return d, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan domains: %w", err)
	}
	return out, nil
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// escapeLike escapes LIKE metacharacters so the query is matched literally.
// Backslash is PostgreSQL's default LIKE escape character.
func escapeLike(s string) string { return likeEscaper.Replace(s) }
