package httpapi

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/reconvault/reconvault-api/internal/artifact"
)

const (
	maxHostnameLength = 255
	maxQueryLength    = 1024
)

// paramError is a query-parameter validation failure.
type paramError struct {
	field, message string
}

func (e *paramError) Error() string { return e.field + ": " + e.message }

func parseLimit(v url.Values, def, max int) (int, error) {
	if !v.Has("limit") {
		return def, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v.Get("limit")))
	if err != nil || n < 1 || n > max {
		return 0, &paramError{"limit", fmt.Sprintf("must be an integer between 1 and %d", max)}
	}
	return n, nil
}

func parseSortOrder(v url.Values, def artifact.SortOrder) (artifact.SortOrder, error) {
	if !v.Has("sort_order") {
		return def, nil
	}
	switch o := artifact.SortOrder(strings.ToUpper(strings.TrimSpace(v.Get("sort_order")))); o {
	case artifact.SortAsc, artifact.SortDesc:
		return o, nil
	default:
		return "", &paramError{"sort_order", "must be ASC or DESC"}
	}
}

func parseSortBy(v url.Values, def string, allowed []string) (string, error) {
	if !v.Has("sort_by") {
		return def, nil
	}
	s := strings.TrimSpace(v.Get("sort_by"))
	if !slices.Contains(allowed, s) {
		return "", &paramError{"sort_by", "must be one of " + strings.Join(allowed, ", ")}
	}
	return s, nil
}

// parseListParams parses the listing query. A cursor supplies the
// defaults for sort_by, sort_order and hostname, so clients can simply
// follow meta.next_cursor; explicit values that contradict the cursor are
// rejected by the service.
func parseListParams(v url.Values) (artifact.ListParams, error) {
	var (
		p   artifact.ListParams
		err error
	)
	sortBy, order, hostname := "requested_at", artifact.SortDesc, ""
	if v.Has("cursor") {
		if p.After, err = decodeCursor(v.Get("cursor")); err != nil {
			return p, err
		}
		sortBy, order, hostname = p.After.SortBy, p.After.SortOrder, p.After.Hostname
	}
	if p.SortBy, err = parseSortBy(v, sortBy, artifact.ListSortFields); err != nil {
		return p, err
	}
	if p.SortOrder, err = parseSortOrder(v, order); err != nil {
		return p, err
	}
	if p.Limit, err = parseLimit(v, artifact.DefaultLimit, artifact.MaxLimit); err != nil {
		return p, err
	}
	p.Hostname = hostname
	if v.Has("hostname") {
		if p.Hostname, err = parseHostname("hostname", v.Get("hostname")); err != nil {
			return p, err
		}
	}
	return p, nil
}

// parseHostname validates and normalizes a hostname parameter. Hostnames
// are stored lower-cased, so the value is lower-cased to match.
func parseHostname(field, raw string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(raw))
	if h == "" || len(h) > maxHostnameLength || !utf8.ValidString(h) || strings.ContainsFunc(h, isControl) {
		return "", &paramError{field, fmt.Sprintf("must be a non-empty hostname of at most %d characters", maxHostnameLength)}
	}
	return h, nil
}

func parseSearchParams(v url.Values) (artifact.SearchParams, error) {
	var (
		p   artifact.SearchParams
		err error
	)
	q := v.Get("q")
	switch {
	case strings.TrimSpace(q) == "":
		return p, &paramError{"q", "is required and must not be blank"}
	case len(q) > maxQueryLength:
		return p, &paramError{"q", fmt.Sprintf("must be at most %d bytes", maxQueryLength)}
	case !utf8.ValidString(q):
		return p, &paramError{"q", "must be valid UTF-8"}
	case strings.ContainsFunc(q, isControl):
		// Also guarantees the query cannot contain the field separator used
		// by the search document.
		return p, &paramError{"q", "must not contain control characters"}
	}
	p.Query = q
	if p.Limit, err = parseLimit(v, artifact.DefaultLimit, artifact.MaxLimit); err != nil {
		return p, err
	}
	return p, nil
}

func parseDomainParams(v url.Values) (artifact.DomainParams, error) {
	var (
		p   artifact.DomainParams
		err error
	)
	if p.SortBy, err = parseSortBy(v, artifact.DomainSortLatestUpdate, artifact.DomainSortFields); err != nil {
		return p, err
	}
	if p.SortOrder, err = parseSortOrder(v, artifact.SortDesc); err != nil {
		return p, err
	}
	if p.Limit, err = parseLimit(v, artifact.DefaultDomainLimit, artifact.MaxDomainLimit); err != nil {
		return p, err
	}
	return p, nil
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }
