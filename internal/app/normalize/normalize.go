// Package normalize is the only place in bodger where raw input from a
// surface becomes a domain value (ADR-0005 "Normalisation is one package,
// and it is the only place ambiguous input becomes a domain value").
//
// Every function here is a pure transformation: given already-known
// values (a resolved currency, an injected clock, a pre-fetched candidate
// list), it produces a domain value or a *errs.Error explaining why it
// couldn't. None of them perform repository I/O themselves — the use-case
// layer (issue #6) fetches whatever a normaliser needs (an account list
// for normalize.Ref, the actor's timezone for normalize.DateOf) and hands
// it in. This keeps every function here cheap to test with plain data and
// keeps the "who does I/O" boundary at the use-case layer, not scattered
// across normalisation helpers.
//
// The one exception is time: normalize.DateOf takes an
// internal/platform/clock.Clock, not an already-resolved instant, because
// ADR-0005 requires that nothing outside internal/platform/clock ever call
// time.Now() — a normaliser that took a time.Time parameter would just
// move the temptation to whoever constructs that time.Time. No function in
// this package calls time.Now() directly.
//
// Every error returned by a function in this package is a *errs.Error
// (ADR-0011): a coarse code, a caller-safe message, and — for the
// functions that need it — a field path or structured details (the
// disambiguation error's candidate list).
package normalize
