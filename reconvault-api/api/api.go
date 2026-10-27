// Package api holds the OpenAPI specification of the HTTP API.
package api

import _ "embed"

// Spec is the OpenAPI 3.0 document (swagger.json), served at /swagger.json.
//
//go:embed swagger.json
var Spec []byte
