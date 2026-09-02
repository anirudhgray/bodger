package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// tokenEntropyBytes is the number of cryptographically random bytes read
// for every session and API token: 256 bits, well above what opaque
// bearer tokens need (ADR-0006 asks only for "sufficient entropy").
const tokenEntropyBytes = 32

// APITokenPrefix prefixes every API token bodger issues, per ADR-0006's
// credential table. It makes a token found in a log line, a shell
// history, or a config file recognisable as bodger's independent of
// context.
const APITokenPrefix = "bdg_"

// GenerateSessionToken returns a new opaque, cryptographically random
// session token for the web UI's session cookie (ADR-0006). The plaintext
// returned here is the only copy ever sent to the browser; callers must
// hash it with HashSessionToken before persisting anything.
func GenerateSessionToken() (string, error) {
	token, err := generateOpaqueToken("")
	if err != nil {
		return "", fmt.Errorf("auth: generate session token: %w", err)
	}
	return token, nil
}

// HashSessionToken returns the SHA-256 hash of a session token, hex
// encoded, as stored server-side. ADR-0006 requires sessions to be
// stored hashed, never in plaintext, so a database compromise does not
// hand over live sessions.
func HashSessionToken(token string) string {
	return hashToken(token)
}

// GenerateAPIToken returns a new opaque, cryptographically random API
// token prefixed with APITokenPrefix, per ADR-0006's credential table.
// The plaintext returned here is shown to the user once, at creation;
// callers must hash it with HashAPIToken before persisting anything.
func GenerateAPIToken() (string, error) {
	token, err := generateOpaqueToken(APITokenPrefix)
	if err != nil {
		return "", fmt.Errorf("auth: generate API token: %w", err)
	}
	return token, nil
}

// HashAPIToken returns the SHA-256 hash of an API token, hex encoded, as
// stored server-side per ADR-0006's credential table.
func HashAPIToken(token string) string {
	return hashToken(token)
}

// generateOpaqueToken reads tokenEntropyBytes of cryptographically random
// data and returns it URL-safe base64 encoded (no padding), prefixed with
// prefix. prefix may be empty.
func generateOpaqueToken(prefix string) (string, error) {
	b := make([]byte, tokenEntropyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the hex-encoded SHA-256 hash of token. Shared by
// HashSessionToken and HashAPIToken: both credential types are opaque
// random strings hashed the same way before storage, so there is exactly
// one hashing implementation to get right.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
