package http

import _ "embed"

// openAPIDocument is bodger's OpenAPI 3 description (issue #8), checked
// in at openapi.json rather than generated from a codegen toolchain —
// ADR-0001 is stdlib-first, and a hand-written description plus a
// structural test (openapi_test.go's TestOpenAPIMatchesRoutes) stays
// closer to that than pulling in a new dependency to generate one would.
// It's JSON, not YAML, for the same reason: the OpenAPI 3 specification
// permits either serialisation for the root document, and JSON is the one
// encoding/json (already stdlib) can parse without adding a YAML library.
//
// milestone 2's web UI generates its TypeScript client from this
// description (ADR-0005 §Enforcement item 4) — that generation is what
// constrains the browser to sending only what the API accepts.
//
//go:embed openapi.json
var openAPIDocument []byte

// OpenAPIDocument returns the exact bytes of openapi.json.
// TestOpenAPIMatchesRoutes is what keeps it honest: it walks routeTable
// (this package's own central route registry) and this document's
// "paths" object and fails the build if the two sets of method+path
// pairs ever diverge, in either direction — so an added, removed, or
// renamed route is a checked-in-CI failure, not a documentation gap
// found by review.
func OpenAPIDocument() []byte {
	return openAPIDocument
}
