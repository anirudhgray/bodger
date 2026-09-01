package http

// This file is package http, not http_test, deliberately: it needs
// routeTable, this package's own unexported central route registry
// (router.go), to compare against openapi.json without maintaining a
// second, exported copy of it just for a test to read.

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// openAPIPathsDoc is the sliver of the full OpenAPI document this test
// actually needs: every path, and the HTTP methods declared under it.
type openAPIPathsDoc struct {
	Paths map[string]map[string]json.RawMessage `json:"paths"`
}

// routeKey renders a route (or an OpenAPI path+method pair) as one
// comparable string, e.g. "GET /api/v1/accounts/{id}". ServeMux's
// "{id}" path-parameter syntax and OpenAPI's are the same brace
// notation, so no translation is needed between the two — this is one of
// the reasons stdlib routing and a hand-written OpenAPI description stay
// easy to keep honest against each other.
func routeKey(method, path string) string {
	return strings.ToUpper(method) + " " + path
}

// TestOpenAPIMatchesRoutes is issue #8's "checked in CI rather than by
// review" requirement for the OpenAPI description: it fails the build if
// routeTable (router.go) and openapi.json's declared paths ever name a
// different set of method+path pairs, in either direction — a route
// added here with no matching OpenAPI entry, or an OpenAPI entry for a
// route that was renamed or removed, both fail this the same way.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	var doc openAPIPathsDoc
	if err := json.Unmarshal(openAPIDocument, &doc); err != nil {
		t.Fatalf("unmarshal openapi.json: %v", err)
	}

	fromSpec := make(map[string]bool)
	for path, methods := range doc.Paths {
		for method := range methods {
			fromSpec[routeKey(method, path)] = true
		}
	}

	fromCode := make(map[string]bool, len(routeTable))
	for _, rt := range routeTable {
		fromCode[routeKey(rt.Method, rt.Pattern)] = true
	}

	var missingFromSpec, staleInSpec []string
	for k := range fromCode {
		if !fromSpec[k] {
			missingFromSpec = append(missingFromSpec, k)
		}
	}
	for k := range fromSpec {
		if !fromCode[k] {
			staleInSpec = append(staleInSpec, k)
		}
	}
	sort.Strings(missingFromSpec)
	sort.Strings(staleInSpec)

	if len(missingFromSpec) > 0 {
		t.Errorf("routes registered in routeTable but not described in openapi.json: %v", missingFromSpec)
	}
	if len(staleInSpec) > 0 {
		t.Errorf("routes described in openapi.json but not registered in routeTable: %v", staleInSpec)
	}
}

// TestOpenAPIDocumentIsWellFormedJSON is a cheap guard that openapi.json
// itself parses — a syntax error there would otherwise only surface as a
// slightly confusing failure in TestOpenAPIMatchesRoutes above.
func TestOpenAPIDocumentIsWellFormedJSON(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(openAPIDocument, &doc); err != nil {
		t.Fatalf("openapi.json is not valid JSON: %v", err)
	}
	if doc["openapi"] == nil {
		t.Error("openapi.json has no top-level \"openapi\" version field")
	}
	if doc["paths"] == nil {
		t.Error("openapi.json has no top-level \"paths\" object")
	}
}

// TestOpenAPIDocumentOnDiskMatchesEmbedded guards against openapi.json
// being edited without a rebuild picking it up in some unusual build
// setup — cheap enough to run always, and it's what actually caught
// go:embed misuse during development.
func TestOpenAPIDocumentOnDiskMatchesEmbedded(t *testing.T) {
	onDisk, err := os.ReadFile("openapi.json")
	if err != nil {
		t.Fatalf("ReadFile(openapi.json): %v", err)
	}
	if string(onDisk) != string(openAPIDocument) {
		t.Error("openapi.json on disk does not match the //go:embed'd bytes")
	}
}
