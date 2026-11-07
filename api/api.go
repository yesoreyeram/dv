// Package api embeds the OpenAPI description served at /api/openapi.json (FR-004).
package api

import _ "embed"

// OpenAPI is the OpenAPI 3.1 document for the dv HTTP API.
//
//go:embed openapi.json
var OpenAPI []byte
