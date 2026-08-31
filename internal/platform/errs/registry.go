// Package errs is bodger's error registry and error type (ADR-0011).
//
// Four surfaces need to report failures — the CLI prints them, the REST API
// returns them with a status, MCP hands them to an agent, and the web UI
// attaches them to a field — and none of those surfaces is allowed to
// decide anything (ADR-0005). So an error has to arrive from the
// application layer already carrying everything each surface needs: a
// stable code, a safe message, a field path, and safe structured details.
//
// The set of codes is closed and flat: eight coarse codes, each with a
// default message, plus a caller-supplied explanation where the default
// isn't specific enough. There is no hierarchy, no nesting, and no
// promotion mechanism — see docs/decisions/0011-error-model.md.
package errs

import "sort"

// Code is one of the eight coarse error codes. The set is closed: every
// value that will ever exist is declared as a constant below. Once shipped,
// a code is a public contract (scripts branch on CLI exit codes, agents
// branch on MCP error codes) — codes are never renamed or removed, only
// added and superseded.
type Code string

// The eight shipped codes, in the order they appear in ADR-0011's mapping
// table. Do not add a ninth without a concrete client that needs to branch
// on it (see ADR-0011's "Eight codes, flat").
const (
	// InvalidInput means the request was malformed or failed validation.
	InvalidInput Code = "invalid_input"
	// NotFound means the named thing doesn't exist.
	NotFound Code = "not_found"
	// Conflict means the request would violate uniqueness or a constraint.
	Conflict Code = "conflict"
	// PreconditionFailed means the request is valid but not allowed in the
	// current state.
	PreconditionFailed Code = "precondition_failed"
	// NotAllowed means the actor may not do this.
	NotAllowed Code = "not_allowed"
	// Unauthenticated means no valid credential was presented.
	Unauthenticated Code = "unauthenticated"
	// Unavailable means a dependency is down or data is missing.
	Unavailable Code = "unavailable"
	// Internal means a bug. Never the user's fault.
	Internal Code = "internal"
)

// entry is one registry row: the default user-facing message for a code,
// plus the surface mapping (HTTP status, CLI exit code) that renders it.
// This is the "surface mapping table — coarse code -> HTTP status and CLI
// exit code — defined once" that ADR-0011 and issue #4 call for: a surface
// looks its code up here rather than choosing a status itself.
type entry struct {
	message    string
	httpStatus int
	cliExit    int
}

// registry is the closed set of coarse codes. Every Code constant above
// must have exactly one entry here — TestRegistry_ShippedCodes in
// registry_test.go pins the set and fails if one is missing, renamed, or
// silently added without updating that test.
var registry = map[Code]entry{
	InvalidInput: {
		message:    "That value isn't valid. Check what you entered and try again.",
		httpStatus: 422,
		cliExit:    2,
	},
	NotFound: {
		message:    "Couldn't find that. Check the name or ID and try again.",
		httpStatus: 404,
		cliExit:    3,
	},
	Conflict: {
		message:    "That conflicts with something that already exists.",
		httpStatus: 409,
		cliExit:    4,
	},
	PreconditionFailed: {
		message:    "That's valid, but not allowed right now given the current state.",
		httpStatus: 412,
		cliExit:    5,
	},
	NotAllowed: {
		message:    "You don't have permission to do that.",
		httpStatus: 403,
		cliExit:    6,
	},
	Unauthenticated: {
		message:    "You need to sign in before doing that.",
		httpStatus: 401,
		cliExit:    7,
	},
	Unavailable: {
		message:    "This isn't available right now. Try again shortly.",
		httpStatus: 503,
		cliExit:    8,
	},
	Internal: {
		message:    "Something went wrong. Check the log for what happened.",
		httpStatus: 500,
		cliExit:    1,
	},
}

// DefaultMessage returns the registry's default user-facing message for
// code, or "" if code isn't one of the eight shipped codes.
func DefaultMessage(code Code) string {
	return registry[code].message
}

// HTTPStatus returns the HTTP status a REST surface should return for code,
// or 0 if code isn't one of the eight shipped codes.
func HTTPStatus(code Code) int {
	return registry[code].httpStatus
}

// CLIExitCode returns the process exit code a CLI surface should return for
// code, or 0 if code isn't one of the eight shipped codes.
func CLIExitCode(code Code) int {
	return registry[code].cliExit
}

// Codes returns every shipped code, sorted for deterministic output. It
// exists for enumeration — tests, and later a surface that needs to list
// what it can branch on — not for hiding the fact that the set is closed
// and small enough to read directly above.
func Codes() []Code {
	out := make([]Code, 0, len(registry))
	for code := range registry {
		out = append(out, code)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
