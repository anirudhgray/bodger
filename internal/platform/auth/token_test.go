package auth_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/anirudhgray/bodger/internal/platform/auth"
)

func TestGenerateSessionToken_HashSessionToken_RoundTrip(t *testing.T) {
	token, err := auth.GenerateSessionToken()
	if err != nil {
		t.Fatalf("GenerateSessionToken() error = %v", err)
	}
	if token == "" {
		t.Fatalf("GenerateSessionToken() = %q, want a non-empty token", token)
	}
	if strings.HasPrefix(token, auth.APITokenPrefix) {
		t.Fatalf("GenerateSessionToken() = %q, unexpectedly carries the API token prefix", token)
	}

	hashed := auth.HashSessionToken(token)
	assertLooksLikeHexSHA256(t, hashed)

	// Hashing is deterministic, and does not simply echo the token back.
	if got := auth.HashSessionToken(token); got != hashed {
		t.Fatalf("HashSessionToken() = %q then %q, want the same hash for the same token", hashed, got)
	}
	if hashed == token {
		t.Fatalf("HashSessionToken() returned the plaintext token unchanged")
	}
}

func TestGenerateAPIToken_HashAPIToken_RoundTrip(t *testing.T) {
	token, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken() error = %v", err)
	}
	if !strings.HasPrefix(token, auth.APITokenPrefix) {
		t.Fatalf("GenerateAPIToken() = %q, want prefix %q per ADR-0006's credential table", token, auth.APITokenPrefix)
	}
	if token == auth.APITokenPrefix {
		t.Fatalf("GenerateAPIToken() = %q, want randomness after the prefix", token)
	}

	hashed := auth.HashAPIToken(token)
	assertLooksLikeHexSHA256(t, hashed)

	if got := auth.HashAPIToken(token); got != hashed {
		t.Fatalf("HashAPIToken() = %q then %q, want the same hash for the same token", hashed, got)
	}
}

func TestGenerateSessionToken_UniqueAcrossCalls(t *testing.T) {
	const n = 200
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		token, err := auth.GenerateSessionToken()
		if err != nil {
			t.Fatalf("GenerateSessionToken() error = %v", err)
		}
		if seen[token] {
			t.Fatalf("GenerateSessionToken() returned a duplicate: %q", token)
		}
		seen[token] = true
	}
}

func TestGenerateAPIToken_UniqueAcrossCalls(t *testing.T) {
	const n = 200
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		token, err := auth.GenerateAPIToken()
		if err != nil {
			t.Fatalf("GenerateAPIToken() error = %v", err)
		}
		if seen[token] {
			t.Fatalf("GenerateAPIToken() returned a duplicate: %q", token)
		}
		seen[token] = true
	}
}

func TestHashSessionToken_HashAPIToken_MalformedInput(t *testing.T) {
	// Token hashing has no notion of a malformed *token* — any string,
	// including the empty string, is a valid opaque credential to hash.
	// The malformed-input case here is the credential a caller must
	// reject before it ever reaches these functions: an empty or
	// obviously-wrong-shaped token still hashes deterministically rather
	// than panicking or erroring, which is exactly why lookups by hash
	// are safe against garbage input from an HTTP header.
	tests := []struct {
		name  string
		token string
	}{
		{name: "empty token", token: ""},
		{name: "token missing the bdg_ prefix", token: "not-a-real-token"},
		{name: "whitespace", token: "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hashed := auth.HashSessionToken(tt.token)
			assertLooksLikeHexSHA256(t, hashed)

			apiHashed := auth.HashAPIToken(tt.token)
			assertLooksLikeHexSHA256(t, apiHashed)

			if hashed != apiHashed {
				t.Fatalf("HashSessionToken(%q) = %q, HashAPIToken(%q) = %q, want identical hashing for the same input", tt.token, hashed, tt.token, apiHashed)
			}
		})
	}
}

func assertLooksLikeHexSHA256(t *testing.T, s string) {
	t.Helper()
	if len(s) != 64 {
		t.Fatalf("hash %q has length %d, want 64 (hex-encoded SHA-256)", s, len(s))
	}
	if _, err := hex.DecodeString(s); err != nil {
		t.Fatalf("hash %q is not valid hex: %v", s, err)
	}
}
