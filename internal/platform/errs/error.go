package errs

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// Error is bodger's error value. Every field except cause reaches the
// user, in some rendering: Code decides the surface mapping (HTTPStatus,
// CLIExitCode), Message and FieldPath attach to the right text or input,
// and Details carries safe structured extras (candidate names, valid
// options).
//
// cause is unexported and deliberately has no accessor: nothing outside
// this package can reach it, and nothing in this package prints it except
// LogValue. Leaking internal detail — a SQL fragment, a file path, a driver
// error — requires writing new code to do it, not forgetting to prevent
// it.
type Error struct {
	Code Code
	// FieldPath is the field or flag this error attaches to, for form and
	// flag attribution — "account_ref", "amount". It is exposed on the
	// wire as "field" (see MarshalJSON) and set via the Field(...) builder
	// method below.
	//
	// It isn't itself named Field: Go doesn't allow a struct field and a
	// method to share a name on the same type, and the builder's fluent
	// Field(path) call is what issue #4 and ADR-0011 specify for setting
	// it, so the method name won this trade.
	FieldPath string
	Message   string
	Details   map[string]any

	cause error
}

// New constructs an Error for code, seeded with the registry's default
// message. code must be one of the eight shipped codes; New panics
// otherwise, because an unregistered code can only reach here by a
// programmer writing errs.New(errs.Code("something-else")) — the eight
// exported constants are the only values any real call site can produce.
func New(code Code) *Error {
	e, ok := registry[code]
	if !ok {
		panic(fmt.Sprintf("errs: %q is not one of the eight shipped codes", code))
	}
	return &Error{Code: code, Message: e.message}
}

// Explain overrides the registry's default message with caller-supplied,
// end-user-safe text. Use it whenever the default isn't specific enough —
// which per ADR-0011 is most of the time: "No account called %q. Did you
// mean %q?" rather than the coarse default.
//
// format is always a fmt format string, exactly like fmt.Errorf — a
// literal "%" in a call with no other arguments must be written "%%". This
// is what lets `go vet` check every call site's verbs against its
// arguments; a conditional "skip formatting when there are no args" would
// defeat that for the one case (a stray "%") it exists to catch.
func (e *Error) Explain(format string, args ...any) *Error {
	e.Message = fmt.Sprintf(format, args...)
	return e
}

// With attaches a structured, end-user-safe detail — candidate names, valid
// options — under key. Never put anything here that shouldn't reach the
// user: Details is serialised in full.
func (e *Error) With(key string, value any) *Error {
	if e.Details == nil {
		e.Details = make(map[string]any, 1)
	}
	e.Details[key] = value
	return e
}

// Field sets the field or flag path this error attaches to, so a REST
// surface can attach it to a form input and a CLI surface can name the
// flag.
func (e *Error) Field(path string) *Error {
	e.FieldPath = path
	return e
}

// Wrap attaches the internal cause — a driver error, a wrapped SQL error,
// anything infrastructure-shaped. It is never serialised and never appears
// in Error(), MarshalJSON, or CLIMessage. It appears only in LogValue, so
// that an internal-coded error still logs its full wrapped cause chain
// (CLAUDE.md's "detailed and specific... the underlying cause/error chain"
// requirement) without that detail ever reaching a surface.
func (e *Error) Wrap(cause error) *Error {
	e.cause = cause
	return e
}

// Error implements the standard error interface. It never includes the
// wrapped cause — only the code and the safe message — so an *Error that
// ends up printed by ordinary Go code (fmt.Println(err), a log line built
// without LogValue support) is still safe to show a user.
func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", string(e.Code), e.Message)
}

// HTTPStatus returns the HTTP status a REST surface should return for this
// error's code. The mapping lives once in the registry (registry.go);
// surfaces call this rather than choosing a status themselves.
func (e *Error) HTTPStatus() int {
	return HTTPStatus(e.Code)
}

// CLIExitCode returns the process exit code a CLI surface should return for
// this error's code.
func (e *Error) CLIExitCode() int {
	return CLIExitCode(e.Code)
}

// CLIMessage renders the error the way a CLI surface should print it: the
// safe message, and nothing else. It never includes the wrapped cause, and
// it never appends FieldPath — FieldPath is an internal identifier
// ("category_ref", "from_account_ref"), meant for a script reading the
// --json field key, not for a person reading the terminal. A message that
// genuinely needs to say which thing it's about should say so in English
// (Explain("No account matches %q.", ref)), not rely on a raw field name
// tacked on afterwards.
func (e *Error) CLIMessage() string {
	return e.Message
}

// wireError is the JSON shape of an Error. It exists so the wire field is
// named "field" (matching ADR-0011's vocabulary) regardless of the Go
// struct field being called FieldPath, and so cause has no path into the
// encoding at all — there's no field here to put it in.
type wireError struct {
	Code    Code           `json:"code"`
	Message string         `json:"message"`
	Field   string         `json:"field,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// MarshalJSON is the JSON encoder path a REST surface renders a 422 (and
// friends) body from. It never includes the wrapped cause.
func (e *Error) MarshalJSON() ([]byte, error) {
	return json.Marshal(wireError{
		Code:    e.Code,
		Message: e.Message,
		Field:   e.FieldPath,
		Details: e.Details,
	})
}

// LogValue implements slog.LogValuer. It is the one place in this package
// that reads cause: passing an *Error to any slog call (logger.Error(msg,
// "error", err)) automatically expands it into a structured group carrying
// the code, message, field, details, and — when a cause was wrapped — its
// full chain, satisfying ADR-0011's "every internal error is logged with
// its full chain" without any surface needing to special-case *Error.
//
// error.Error() on a wrapped chain built with fmt.Errorf's %w already
// includes every level's text, so logging cause.Error() here is the full
// chain, not just the innermost frame.
func (e *Error) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.String("code", string(e.Code)),
		slog.String("message", e.Message),
	}
	if e.FieldPath != "" {
		attrs = append(attrs, slog.String("field", e.FieldPath))
	}
	if len(e.Details) > 0 {
		attrs = append(attrs, slog.Any("details", e.Details))
	}
	if e.cause != nil {
		attrs = append(attrs, slog.String("cause", e.cause.Error()))
	}
	return slog.GroupValue(attrs...)
}
