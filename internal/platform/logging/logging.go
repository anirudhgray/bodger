// Package logging provides bodger's structured logger, via log/slog. Every
// surface and the app layer share this constructor, so log shape doesn't
// drift between them.
//
// Two rules govern what goes in a log line here:
//
//  1. No PII, no amounts, no payee or description text (CLAUDE.md). This
//     package cannot enforce that mechanically — like the vocabulary and
//     fmt.Errorf-in-app-layer rules described in docs/contributing.md, it
//     is a review responsibility until a lint exists for it.
//  2. An *errs.Error's wrapped cause belongs only in the log, never on a
//     surface (ADR-0011). This package needs no special handling for
//     that: *errs.Error implements slog.LogValuer
//     (internal/platform/errs.Error.LogValue), so passing one to
//     logger.Error(msg, "error", err) already renders its full cause
//     chain — slog dispatches to LogValue on its own.
package logging

import (
	"io"
	"log/slog"
)

// New constructs a structured JSON logger writing to w at the given
// minimum level.
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}
