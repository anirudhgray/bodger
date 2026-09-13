package mcp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// confirmationTokenTTL is how long a confirmation token issued for a
// destructive-tier tool stays valid — ADR-0013's "short-lived, single-use
// confirmation_token". Session-scoped in spirit (the ADR's own
// "in-memory is sufficient since a token's lifetime is a single MCP
// session"): a token issued in one `bodger mcp` process is never valid in
// another, since tokenStore itself is never persisted.
const confirmationTokenTTL = 5 * time.Minute

// confirmationTokenArgKey is the reserved top-level argument key a client
// attaches to a destructive tool's second call to supply the token
// issued by its first. It's stripped from the arguments before they ever
// reach a ToolDef's Describe/Execute or the audit log — it's
// call-protocol plumbing, not a tool argument.
const confirmationTokenArgKey = "confirmation_token"

// splitConfirmationToken extracts confirmationTokenArgKey from raw (if
// present) and returns it alongside the remaining arguments, canonicalised
// by an unmarshal/remarshal round trip through a map — encoding/json
// sorts a map's string keys on Marshal, so two calls with the same
// arguments in different key order produce the same canonical bytes. An
// empty raw is treated as "no arguments" rather than an error, since a
// tool that takes no arguments (whoami) is called with none at all.
func splitConfirmationToken(raw json.RawMessage) (token string, businessArgs json.RawMessage, err error) {
	if len(raw) == 0 {
		return "", []byte("{}"), nil
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", nil, fmt.Errorf("mcp: decode tool arguments: %w", err)
	}
	if tokRaw, ok := m[confirmationTokenArgKey]; ok {
		if err := json.Unmarshal(tokRaw, &token); err != nil {
			return "", nil, fmt.Errorf("mcp: decode %s: %w", confirmationTokenArgKey, err)
		}
		delete(m, confirmationTokenArgKey)
	}

	canon, err := json.Marshal(m)
	if err != nil {
		return "", nil, fmt.Errorf("mcp: re-encode tool arguments: %w", err)
	}
	return token, canon, nil
}

// argsFingerprint hashes businessArgs (as produced by
// splitConfirmationToken) so tokenStore never has to hold a copy of the
// arguments themselves — only enough to notice if they change between the
// issuing call and the confirming one.
func argsFingerprint(businessArgs json.RawMessage) [32]byte {
	return sha256.Sum256(businessArgs)
}

// tokenRecord is one issued-but-not-yet-consumed confirmation token.
type tokenRecord struct {
	toolName  string
	argsHash  [32]byte
	expiresAt time.Time
}

// tokenStore holds every destructive-tier confirmation token issued by
// this dispatcher, in memory, for the lifetime of one `bodger mcp`
// process (ADR-0013: "in-memory is sufficient since a token's lifetime is
// a single MCP session"). Safe for concurrent use — the SDK may invoke
// tool handlers for a session's overlapping requests concurrently.
type tokenStore struct {
	mu     sync.Mutex
	clock  clock.Clock
	ttl    time.Duration
	tokens map[string]tokenRecord
}

func newTokenStore(clk clock.Clock, ttl time.Duration) *tokenStore {
	return &tokenStore{clock: clk, ttl: ttl, tokens: make(map[string]tokenRecord)}
}

// issue mints a new single-use token for toolName, bound to
// businessArgs — the arguments a subsequent confirming call must match
// exactly to consume it.
func (s *tokenStore) issue(toolName string, businessArgs json.RawMessage) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	token := randomToken()
	s.tokens[token] = tokenRecord{
		toolName:  toolName,
		argsHash:  argsFingerprint(businessArgs),
		expiresAt: s.clock.Now().Add(s.ttl),
	}
	return token
}

// consume validates and burns token in one step: it is removed from the
// store whether or not it turns out to be valid, so a single-use token
// can never be retried after a failed match — ADR-0013's "rejected if
// reused, expired, or attached to a call whose arguments don't match" all
// end the token's life, not just a successful consumption.
func (s *tokenStore) consume(toolName, token string, businessArgs json.RawMessage) *errs.Error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.tokens[token]
	if !ok {
		return errs.New(errs.PreconditionFailed).
			Explain("This confirmation token is unknown or has already been used. Call %s again without a token to get a new one.", toolName)
	}
	delete(s.tokens, token)

	if rec.toolName != toolName {
		return errs.New(errs.PreconditionFailed).
			Explain("This confirmation token was issued for a different tool.")
	}
	if s.clock.Now().After(rec.expiresAt) {
		return errs.New(errs.PreconditionFailed).
			Explain("This confirmation token has expired. Call %s again without a token to get a new one.", toolName)
	}
	if rec.argsHash != argsFingerprint(businessArgs) {
		return errs.New(errs.PreconditionFailed).
			Explain("The arguments for %s have changed since this confirmation was issued. Call it again without a token to reconfirm.", toolName)
	}
	return nil
}

// randomToken returns a fresh 32-hex-character random token. crypto/rand
// is used, not the app layer's idgen.Generator: a confirmation token is a
// transient security credential scoped to one process's in-memory
// tokenStore, not a persisted domain entity's ID — the two have no
// business sharing a generator.
func randomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read only fails if the OS's random source is
		// broken, which nothing in this codebase can recover from
		// meaningfully — the same reasoning idgen.UUID's own dependency
		// (google/uuid) accepts for the same failure mode.
		panic("mcp: reading random bytes for a confirmation token: " + err.Error())
	}
	return hex.EncodeToString(b)
}
