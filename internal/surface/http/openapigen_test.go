package http

// This file is package http, not http_test, for the same reason
// openapi_test.go is: it needs routeTable and this package's own
// unexported DTOs to regenerate the document it's checking, without
// exporting either just for a test to reach them.

import (
	"testing"
)

// TestOpenAPIDocumentMatchesGenerator is issue #36's CI freshness check:
// it re-runs GenerateOpenAPIDocument (openapi_gen.go) and fails the build
// if the result doesn't match openapi.json on disk byte-for-byte. A DTO
// or routeTable change with no matching `go generate` (see
// docs/contributing.md) fails here, in CI, rather than shipping a stale
// document that silently drifts from the code that's supposed to define
// it.
func TestOpenAPIDocumentMatchesGenerator(t *testing.T) {
	got, err := GenerateOpenAPIDocument()
	if err != nil {
		t.Fatalf("GenerateOpenAPIDocument: %v", err)
	}
	if string(got) != string(openAPIDocument) {
		t.Error("openapi.json is stale: it doesn't match a fresh run of the generator. Run `go generate ./...` (see docs/contributing.md) and commit the result.")
	}
}

// TestOpenAPIEnumsCoverErrorCodes guards enumSets' "error_code" entry —
// the one enum set actually derived from code (errs.Codes()) rather than
// hand-maintained — against a bug that would leave it empty and so
// silently accept any code value in the generated schema.
func TestOpenAPIEnumsCoverErrorCodes(t *testing.T) {
	if len(enumSets["error_code"]) == 0 {
		t.Fatal(`enumSets["error_code"] is empty — errs.Codes() returned nothing`)
	}
}
