package http

import _ "embed"

// openAPIDocument is bodger's OpenAPI 3 description (issue #8), checked
// in at openapi.json — but generated, not hand-written (issue #36):
// openapi_gen.go's GenerateOpenAPIDocument builds it by reflecting over
// this package's own request/response DTOs and walking routeTable, and
// cmd/genopenapi (wired via the //go:generate directive on
// GenerateOpenAPIDocument) is what actually regenerates the checked-in
// file — see docs/contributing.md. It's still JSON, not YAML: the
// OpenAPI 3 specification permits either serialisation for the root
// document, and JSON is the one encoding/json (already stdlib) needs at
// runtime for this file's own embedding below.
//
// milestone 2's web UI generates its TypeScript client from this
// description (ADR-0005 §Enforcement item 4) — that generation is what
// constrains the browser to sending only what the API accepts.
//
//go:embed openapi.json
var openAPIDocument []byte

// OpenAPIDocument returns the exact bytes of openapi.json.
// openapigen_test.go's TestOpenAPIDocumentMatchesGenerator is what keeps
// it honest: it re-runs GenerateOpenAPIDocument and fails the build if
// the result doesn't match this file byte-for-byte, so a DTO or
// routeTable change with no matching `go generate` fails CI rather than
// shipping a stale document. TestOpenAPIMatchesRoutes (openapi_test.go)
// is a second, narrower check in the same spirit, kept as cheap
// insurance against openapi.json ever being hand-edited out of step with
// routeTable directly.
func OpenAPIDocument() []byte {
	return openAPIDocument
}
