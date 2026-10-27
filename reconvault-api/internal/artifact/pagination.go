package artifact

import (
	"strconv"
	"time"
)

// ListCursor identifies the last row of a listing page. The next page
// contains the rows that sort strictly after it (keyset pagination), so
// pages stay stable while new artifacts are being recorded.
type ListCursor struct {
	SortBy    string
	SortOrder SortOrder
	Hostname  string
	// Value is the last row's sort column value in its canonical text form
	// (see SortValue); nil represents SQL NULL.
	Value *string
	// ID is the last row's id, the tie-breaker of every sort order.
	ID string
}

// ListPage is one page of an artifact listing.
type ListPage struct {
	Items []Summary
	// Next is the cursor of the following page, or nil on the last page.
	Next *ListCursor
}

// sortKind is the type of a sortable column.
type sortKind int

const (
	kindText sortKind = iota
	kindInt
	kindTime
)

type sortField struct {
	kind     sortKind
	nullable bool
}

// listSortFieldInfo describes every value of ListSortFields.
var listSortFieldInfo = map[string]sortField{
	"http_method":              {kindText, false},
	"full_url":                 {kindText, false},
	"response_status_code":     {kindInt, true},
	"response_content_type":    {kindText, true},
	"response_body_size_bytes": {kindInt, true},
	"requested_at":             {kindTime, false},
}

// SortFieldNullable reports whether the sort column can contain NULL.
func SortFieldNullable(field string) bool { return listSortFieldInfo[field].nullable }

// SortValue returns the canonical text form of the summary's value for the
// given sort field, or nil when the value is NULL.
func (s Summary) SortValue(field string) *string {
	switch field {
	case "http_method":
		return &s.HTTPMethod
	case "full_url":
		return &s.FullURL
	case "response_status_code":
		if s.ResponseStatusCode == nil {
			return nil
		}
		return ptr(strconv.Itoa(*s.ResponseStatusCode))
	case "response_content_type":
		return s.ResponseContentType
	case "response_body_size_bytes":
		if s.ResponseBodySizeBytes == nil {
			return nil
		}
		return ptr(strconv.FormatInt(*s.ResponseBodySizeBytes, 10))
	case "requested_at":
		return ptr(s.RequestedAt.UTC().Format(time.RFC3339Nano))
	}
	return nil
}

// ParseSortValue converts a cursor value to the Go type of the sort column
// (string, int64 or time.Time); nil stays nil (SQL NULL).
func ParseSortValue(field string, v *string) (any, error) {
	info, ok := listSortFieldInfo[field]
	if !ok {
		return nil, invalid("cursor", "has an unknown sort field")
	}
	if v == nil {
		if !info.nullable {
			return nil, invalid("cursor", "has a null value for a non-nullable sort field")
		}
		return nil, nil
	}
	switch info.kind {
	case kindInt:
		n, err := strconv.ParseInt(*v, 10, 64)
		if err != nil {
			return nil, invalid("cursor", "has an invalid value")
		}
		return n, nil
	case kindTime:
		t, err := time.Parse(time.RFC3339Nano, *v)
		if err != nil {
			return nil, invalid("cursor", "has an invalid value")
		}
		return t, nil
	default:
		return *v, nil
	}
}

// Validate checks that the cursor is internally consistent.
func (c *ListCursor) Validate() error {
	if c.SortOrder != SortAsc && c.SortOrder != SortDesc {
		return invalid("cursor", "has an invalid sort order")
	}
	if !IsUUID(c.ID) {
		return invalid("cursor", "has an invalid id")
	}
	_, err := ParseSortValue(c.SortBy, c.Value)
	return err
}
