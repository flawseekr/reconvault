package httpapi

import (
	"encoding/base64"
	"encoding/json"

	"github.com/reconvault/reconvault-api/internal/artifact"
)

// maxCursorLength bounds the encoded cursor. The largest cursor carries a
// maximum-length full_url (8192 bytes) plus JSON and base64 overhead.
const maxCursorLength = 16 << 10

// cursorToken is the JSON form of a cursor. Clients treat the base64url
// encoding as opaque; it is validated on every use and only ever reaches
// SQL as bind parameters.
type cursorToken struct {
	SortBy    string             `json:"s"`
	SortOrder artifact.SortOrder `json:"o"`
	Hostname  string             `json:"h,omitempty"`
	Value     *string            `json:"v"`
	ID        string             `json:"i"`
}

func encodeCursor(c *artifact.ListCursor) string {
	b, _ := json.Marshal(cursorToken{ // cannot fail: only strings
		SortBy: c.SortBy, SortOrder: c.SortOrder, Hostname: c.Hostname, Value: c.Value, ID: c.ID,
	})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (*artifact.ListCursor, error) {
	if s == "" || len(s) > maxCursorLength {
		return nil, &paramError{"cursor", "is invalid"}
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, &paramError{"cursor", "is invalid"}
	}
	var t cursorToken
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, &paramError{"cursor", "is invalid"}
	}
	c := &artifact.ListCursor{SortBy: t.SortBy, SortOrder: t.SortOrder, Hostname: t.Hostname, Value: t.Value, ID: t.ID}
	if err := c.Validate(); err != nil {
		return nil, &paramError{"cursor", "is invalid"}
	}
	return c, nil
}
