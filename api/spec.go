// Package api holds the API contract, openapi.yaml, embedded so the server's
// MCP endpoint can offer operations of it as tools with the names,
// descriptions, permissions and inputs the contract gives them.
package api

import _ "embed"

// OpenAPI is api/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPI []byte
