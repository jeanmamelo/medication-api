// Package docs embeds the OpenAPI spec so it can be served directly by the API binary.
package docs

import _ "embed"

// OpenAPISpec is the contents of openapi.yaml, served at GET /openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPISpec []byte
