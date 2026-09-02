package http

// This file builds internal/surface/http/openapi.json from routeTable
// (router.go) and this package's own request/response DTOs — issue #36's
// move from a hand-written document to a generated one. GenerateOpenAPIDocument
// is the whole thing in one call; WriteOpenAPIDocument writes its result
// back to openapi.json, and is what the go:generate directive below
// invokes via cmd/genopenapi. openapigen_test.go is the CI freshness
// check: it re-runs GenerateOpenAPIDocument and fails the build if the
// result doesn't match openapi.json on disk, so a DTO or routeTable
// change with no matching `go generate` fails CI rather than shipping a
// stale document.
//
//go:generate go run github.com/anirudhgray/bodger/cmd/genopenapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// errorView mirrors errs.wireError's JSON shape (internal/platform/errs/error.go)
// for OpenAPI generation purposes only. It's needed because errs.wireError
// itself is unexported (nothing outside errs should construct one by
// hand), and *errs.Error's own exported fields don't match the wire shape
// its MarshalJSON actually produces (FieldPath vs. "field", an unexported
// cause with no wire representation at all). Nothing here is ever
// constructed or marshalled at runtime — errs.Error.MarshalJSON is what
// actually renders every error response this surface sends; this struct
// only stands in for its shape during schema generation, and is kept in
// sync with it by code review, the same way router.go's routeTable is
// kept in sync with the handlers it names.
type errorView struct {
	Code    string         `json:"code" enum:"error_code"`
	Message string         `json:"message"`
	Field   string         `json:"field,omitempty" doc:"The request field or CLI flag this error attaches to, when there is one."`
	Details map[string]any `json:"details,omitempty" doc:"Safe, structured extra context - candidate names, valid options."`
}

// enumSets is every closed value set an `enum:"..."` struct tag in this
// package's DTOs can name (dto.go, accounts.go, categories.go,
// transactions.go, healthz.go). It's the one place those sets are
// spelled out for documentation purposes — this package deliberately
// never imports internal/domain/ledger (see http.go's doc comment), so
// these mirror ledger.AccountKind/CategoryKind's wire values rather than
// being derived from them; keeping them in sync is a code-review
// discipline, the same as errorView above. "error_code" is the one
// exception: it's derived from errs.Codes() directly below, since errs
// isn't a domain package and is already this package's own dependency.
var enumSets = map[string][]string{
	"account_kind":                {"bank", "cash", "credit_card", "wallet", "investment", "loan", "other"},
	"category_kind":               {"expense", "income"},
	"transaction_kind":            {"outflow", "inflow", "transfer"},
	"recordable_transaction_kind": {"outflow", "inflow"},
	"health_status":               {"ok"},
	"error_code":                  errorCodeValues(),
}

func errorCodeValues() []string {
	codes := errs.Codes()
	out := make([]string, len(codes))
	for i, c := range codes {
		out[i] = string(c)
	}
	return out
}

// errorResponseNames maps an error status code declared on a routeTable
// entry to the components.responses entry GenerateOpenAPIDocument
// registers for it. Every status a route's Errors field names must have
// an entry here, or generation fails loudly rather than silently omitting
// the response.
var errorResponseNames = map[int]string{
	http.StatusNotFound:            "NotFound",
	http.StatusUnprocessableEntity: "InvalidInput",
}

// schemaTypeName is this generator's naming rule for a Go DTO type's
// OpenAPI component name: an unexported bodger-convention name like
// accountView or createAccountRequest becomes the exported name a
// generated client sees — Account, CreateAccountRequest. Stripping a
// trailing "View" is the only transformation needed because every
// *request struct is already named the way it should read on the wire
// (createAccountRequest -> CreateAccountRequest), and every *view struct
// is named for its Go role, not its wire role (accountView -> Account).
// Returns "" for an unnamed type (a slice or map literal), the same
// signal openapi3gen itself uses for "inline this, don't componentize
// it".
func schemaTypeName(t reflect.Type) string {
	n := t.Name()
	if n == "" {
		return ""
	}
	n = strings.TrimSuffix(n, "View")
	return strings.ToUpper(n[:1]) + n[1:]
}

// schemaCustomizer is the openapi3gen.SchemaCustomizerFn (issue #36's
// "SchemaCustomizer hook") that reads this package's own `enum`, `doc`,
// and `format` struct tags (see dto.go's doc comment) and enriches the
// reflected schema with what a plain Go type can't carry on its own.
func schemaCustomizer(_ string, t reflect.Type, tag reflect.StructTag, schema *openapi3.Schema) error {
	if t.Kind() == reflect.Interface {
		// openapi3gen reuses the parent field's own name and struct tag
		// for a map's value schema (e.g. Details map[string]any) — skip
		// customizing the free-form `any` placeholder itself, or its
		// schema would repeat whatever's on the map field.
		return nil
	}
	if doc, ok := tag.Lookup("doc"); ok {
		schema.Description = doc
	}
	if enumName, ok := tag.Lookup("enum"); ok {
		values, known := enumSets[enumName]
		if !known {
			return fmt.Errorf("enum tag %q names no set in enumSets", enumName)
		}
		schema.Enum = make([]any, len(values))
		for i, v := range values {
			schema.Enum[i] = v
		}
	}
	if format, ok := tag.Lookup("format"); ok {
		schema.Format = format
		if schema.Description == "" && format == "money" {
			schema.Description = "A plain decimal amount. Always a JSON string, in the sibling currency field's currency - never a number."
		}
	}
	return nil
}

// applyRequired sets schema's "required" list from t's own json struct
// tags: any exported field with a json tag that doesn't say "omitempty"
// is required. openapi3gen never computes this itself (it has no notion
// of "required" at all), so every top-level schema this generator
// registers gets it filled in here instead, once, right after
// generation. It always overwrites rather than appends, so calling it
// more than once for the same type (every DTO referenced by more than
// one route hits this once per route) stays idempotent rather than
// accumulating duplicates.
func applyRequired(schema *openapi3.Schema, t reflect.Type) {
	if t.Kind() != reflect.Struct {
		return
	}
	required := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported
		}
		jsonTag, ok := f.Tag.Lookup("json")
		if !ok {
			continue // openapi3gen itself skips fields with no json tag
		}
		name, rest, _ := strings.Cut(jsonTag, ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		if strings.Contains(","+rest+",", ",omitempty,") {
			continue
		}
		required = append(required, name)
	}
	schema.Required = required
}

// schemaForBody generates v's schema via gen, registering it (and
// anything it references) into schemas, and applies applyRequired to
// every named struct schema reachable from v's type — v's own type,
// every struct field's type, recursively, and the element type of any
// slice or map along the way (a list response's items, e.g.
// []accountView; or a nested response like balancesView, whose
// []balanceView field registers its own "Balance" schema that needs the
// same treatment as a top-level one).
func schemaForBody(gen *openapi3gen.Generator, schemas openapi3.Schemas, v any) (*openapi3.SchemaRef, error) {
	ref, err := gen.NewSchemaRefForValue(v, schemas)
	if err != nil {
		return nil, err
	}
	applyRequiredRecursive(schemas, reflect.TypeOf(v), make(map[reflect.Type]bool))
	return ref, nil
}

// applyRequiredRecursive walks t (and, for a struct, every field's own
// type) applying applyRequired to each named struct type's registered
// component schema. seen guards against revisiting a type this generator
// has already handled — both for efficiency and because a self- or
// mutually-referential type graph would otherwise recurse forever.
func applyRequiredRecursive(schemas openapi3.Schemas, t reflect.Type, seen map[reflect.Type]bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if seen[t] {
		return
	}
	seen[t] = true

	switch t.Kind() {
	case reflect.Struct:
		if name := schemaTypeName(t); name != "" {
			if s, ok := schemas[name]; ok && s.Value != nil {
				applyRequired(s.Value, t)
			}
		}
		for i := range t.NumField() {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue // unexported
			}
			if _, ok := f.Tag.Lookup("json"); !ok {
				continue // openapi3gen itself skips fields with no json tag
			}
			applyRequiredRecursive(schemas, f.Type, seen)
		}
	case reflect.Slice, reflect.Array, reflect.Map:
		applyRequiredRecursive(schemas, t.Elem(), seen)
	}
}

// envelopeName is the components.schemas name for v's success-response
// envelope: dataEnvelope{"data": v} wraps every success response this
// surface sends (respond.go), and this generator gives that wrapper its
// own named schema rather than inlining it at every response site —
// "AccountEnvelope", or "AccountListEnvelope" when v is a slice.
func envelopeName(t reflect.Type) string {
	if t.Kind() == reflect.Slice {
		return schemaTypeName(t.Elem()) + "ListEnvelope"
	}
	return schemaTypeName(t) + "Envelope"
}

// registerEnvelope registers name as a components.schemas object with a
// single required property, wrapping dataRef under key — "data" for a
// success envelope, "error" for errorView's — and returns a $ref to it.
func registerEnvelope(schemas openapi3.Schemas, name, key string, dataRef *openapi3.SchemaRef) *openapi3.SchemaRef {
	schemas[name] = &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type:       &openapi3.Types{"object"},
		Properties: openapi3.Schemas{key: dataRef},
		Required:   []string{key},
	}}
	return &openapi3.SchemaRef{Ref: "#/components/schemas/" + name}
}

// idParameter is the "id" path parameter every /{id} route shares —
// registered once, in components.parameters, and $ref'd from each
// operation rather than repeated inline.
func idParameter() *openapi3.Parameter {
	return openapi3.NewPathParameter("id").
		WithDescription("The resource's ID, or (for an account or category) its unique name.").
		WithSchema(openapi3.NewStringSchema())
}

// queryParameter builds q's OpenAPI parameter object.
func queryParameter(q queryParam) *openapi3.Parameter {
	schema := &openapi3.Schema{Type: &openapi3.Types{q.Type}}
	if q.Format != "" {
		schema.Format = q.Format
	}
	if len(q.Enum) > 0 {
		schema.Enum = make([]any, len(q.Enum))
		for i, v := range q.Enum {
			schema.Enum[i] = v
		}
	}
	if q.Min != nil {
		v := float64(*q.Min)
		schema.Min = &v
	}
	if q.Max != nil {
		v := float64(*q.Max)
		schema.Max = &v
	}
	return openapi3.NewQueryParameter(q.Name).WithDescription(q.Description).WithSchema(schema)
}

// errorResponse builds one components.responses entry: description plus
// a JSON body of errorEnvelopeRef.
func errorResponse(description string, errorEnvelopeRef *openapi3.SchemaRef) *openapi3.ResponseRef {
	return &openapi3.ResponseRef{Value: openapi3.NewResponse().
		WithDescription(description).
		WithContent(openapi3.NewContentWithJSONSchemaRef(errorEnvelopeRef)),
	}
}

// buildOperation builds rt's OpenAPI operation: its parameters (the
// shared "id" path parameter, then rt.Query in declared order), its
// request body when rt.Request is set, and its responses — one success
// response wrapping rt.Response in a named envelope, plus a $ref per
// declared error status.
func buildOperation(gen *openapi3gen.Generator, schemas openapi3.Schemas, rt route) (*openapi3.Operation, error) {
	op := &openapi3.Operation{
		OperationID: rt.OperationID,
		Summary:     rt.Summary,
		Description: rt.Description,
	}

	if strings.Contains(rt.Pattern, "{id}") {
		op.Parameters = append(op.Parameters, &openapi3.ParameterRef{Ref: "#/components/parameters/Id"})
	}
	for _, q := range rt.Query {
		op.Parameters = append(op.Parameters, &openapi3.ParameterRef{Value: queryParameter(q)})
	}

	if rt.Request != nil {
		reqRef, err := schemaForBody(gen, schemas, rt.Request)
		if err != nil {
			return nil, fmt.Errorf("request schema: %w", err)
		}
		op.RequestBody = &openapi3.RequestBodyRef{
			Value: openapi3.NewRequestBody().WithRequired(true).WithJSONSchemaRef(reqRef),
		}
	}

	respRef, err := schemaForBody(gen, schemas, rt.Response)
	if err != nil {
		return nil, fmt.Errorf("response schema: %w", err)
	}
	envRef := registerEnvelope(schemas, envelopeName(reflect.TypeOf(rt.Response)), "data", respRef)

	responses := &openapi3.Responses{}
	responses.Set(strconv.Itoa(rt.SuccessStatus), &openapi3.ResponseRef{
		Value: openapi3.NewResponse().
			WithDescription(rt.SuccessDescription).
			WithContent(openapi3.NewContentWithJSONSchemaRef(envRef)),
	})
	for _, code := range rt.Errors {
		name, ok := errorResponseNames[code]
		if !ok {
			return nil, fmt.Errorf("status %d has no entry in errorResponseNames", code)
		}
		responses.Set(strconv.Itoa(code), &openapi3.ResponseRef{Ref: "#/components/responses/" + name})
	}
	op.Responses = responses

	return op, nil
}

// GenerateOpenAPIDocument builds bodger's OpenAPI 3 description entirely
// from routeTable (router.go) and this package's DTOs — see this file's
// own doc comment. Its result is what WriteOpenAPIDocument writes to
// openapi.json and what openapigen_test.go compares openapi.json against.
func GenerateOpenAPIDocument() ([]byte, error) {
	gen := openapi3gen.NewGenerator(
		openapi3gen.CreateComponentSchemas(openapi3gen.ExportComponentSchemasOptions{
			ExportComponentSchemas: true,
			ExportTopLevelSchema:   true,
		}),
		openapi3gen.CreateTypeNameGenerator(func(t reflect.Type) string {
			if n := schemaTypeName(t); n != "" {
				return n
			}
			return t.Name()
		}),
		openapi3gen.SchemaCustomizer(schemaCustomizer),
	)

	schemas := make(openapi3.Schemas)
	errRef, err := schemaForBody(gen, schemas, errorView{})
	if err != nil {
		return nil, fmt.Errorf("error schema: %w", err)
	}
	errorEnvelopeRef := registerEnvelope(schemas, "ErrorEnvelope", "error", errRef)

	pathItems := make(map[string]*openapi3.PathItem, len(routeTable))
	for _, rt := range routeTable {
		op, err := buildOperation(gen, schemas, rt)
		if err != nil {
			return nil, fmt.Errorf("route %s %s: %w", rt.Method, rt.Pattern, err)
		}
		item, ok := pathItems[rt.Pattern]
		if !ok {
			item = &openapi3.PathItem{}
			pathItems[rt.Pattern] = item
		}
		item.SetOperation(rt.Method, op)
	}

	paths := openapi3.NewPaths()
	for pattern, item := range pathItems {
		paths.Set(pattern, item)
	}

	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info: &openapi3.Info{
			Title:   "bodger REST API",
			Version: "1.0.0",
			Description: "bodger's read/write REST API. This is the web UI's only interface " +
				"(milestone 2 generates its TypeScript client from this description) and is " +
				"available to external clients and integrations. There is no authentication yet: " +
				"the server binds a loopback address only, and refuses to start otherwise. " +
				"Monetary values are always JSON strings, never numbers.",
		},
		Servers: openapi3.Servers{{URL: "/"}},
		Paths:   paths,
		Components: &openapi3.Components{
			Schemas: schemas,
			Parameters: openapi3.ParametersMap{
				"Id": &openapi3.ParameterRef{Value: idParameter()},
			},
			Responses: openapi3.ResponseBodies{
				"NotFound":     errorResponse("No such resource.", errorEnvelopeRef),
				"InvalidInput": errorResponse("The request failed validation.", errorEnvelopeRef),
			},
		},
	}

	if err := openapi3.NewLoader().ResolveRefsIn(doc, nil); err != nil {
		return nil, fmt.Errorf("resolve generated document's own refs: %w", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("generated document is invalid: %w", err)
	}

	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal generated document: %w", err)
	}
	return append(body, '\n'), nil
}

// openAPIDocumentPath resolves openapi.json's path relative to this
// source file's own directory (via runtime.Caller) rather than the
// process's working directory — go:generate always runs with the
// declaring file's directory as its working directory, but this keeps
// WriteOpenAPIDocument correct however cmd/genopenapi is actually
// invoked (`go generate ./...` from the repo root included).
func openAPIDocumentPath() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("openapi_gen.go: runtime.Caller(0) failed")
	}
	return filepath.Join(filepath.Dir(file), "openapi.json"), nil
}

// WriteOpenAPIDocument regenerates openapi.json in place. cmd/genopenapi
// is the only caller — this is the //go:generate-wired entry point this
// file's own doc comment describes.
func WriteOpenAPIDocument() error {
	body, err := GenerateOpenAPIDocument()
	if err != nil {
		return err
	}
	path, err := openAPIDocumentPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}
