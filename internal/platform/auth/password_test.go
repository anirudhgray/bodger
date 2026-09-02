package auth_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/anirudhgray/bodger/internal/platform/auth"
)

func TestHashPassword_VerifyPassword_RoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		password string
		params   auth.Argon2Params
	}{
		{
			name:     "default params",
			password: "correct horse battery staple",
			params:   auth.DefaultArgon2Params,
		},
		{
			name:     "empty password",
			password: "",
			params:   auth.DefaultArgon2Params,
		},
		{
			name:     "unicode password",
			password: "Ünïcödé 密码 🔒",
			params:   auth.DefaultArgon2Params,
		},
		{
			name:     "smaller cost params",
			password: "a lower-cost password for fast tests",
			params: auth.Argon2Params{
				Memory:      8 * 1024,
				Iterations:  1,
				Parallelism: 1,
				SaltLength:  16,
				KeyLength:   32,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := auth.HashPassword(tt.password, tt.params)
			if err != nil {
				t.Fatalf("HashPassword() error = %v", err)
			}

			ok, err := auth.VerifyPassword(tt.password, encoded)
			if err != nil {
				t.Fatalf("VerifyPassword() error = %v, want nil", err)
			}
			if !ok {
				t.Fatalf("VerifyPassword() = false, want true for the password just hashed")
			}

			ok, err = auth.VerifyPassword(tt.password+"wrong", encoded)
			if err != nil {
				t.Fatalf("VerifyPassword() with wrong password error = %v, want nil", err)
			}
			if ok {
				t.Fatalf("VerifyPassword() = true, want false for a mismatched password")
			}
		})
	}
}

func TestHashPassword_ProducesSelfDescribingPHCString(t *testing.T) {
	encoded, err := auth.HashPassword("hunter2", auth.DefaultArgon2Params)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if !strings.HasPrefix(encoded, "$argon2id$v=") {
		t.Fatalf("HashPassword() = %q, want a $argon2id$v=... PHC string", encoded)
	}
	if got := strings.Count(encoded, "$"); got != 5 {
		t.Fatalf("HashPassword() = %q has %d '$' separators, want 5", encoded, got)
	}
}

func TestHashPassword_DistinctSaltsProduceDistinctHashes(t *testing.T) {
	const password = "same password, different salt"

	first, err := auth.HashPassword(password, auth.DefaultArgon2Params)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	second, err := auth.HashPassword(password, auth.DefaultArgon2Params)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if first == second {
		t.Fatalf("HashPassword() returned identical hashes for two calls with the same password; salts should differ")
	}

	// Both must still independently verify.
	for _, encoded := range []string{first, second} {
		ok, err := auth.VerifyPassword(password, encoded)
		if err != nil || !ok {
			t.Fatalf("VerifyPassword(%q) = (%v, %v), want (true, nil)", encoded, ok, err)
		}
	}
}

func TestVerifyPassword_MalformedInput(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
	}{
		{name: "empty string", encoded: ""},
		{name: "not a PHC string at all", encoded: "not-a-hash"},
		{name: "wrong algorithm", encoded: "$argon2i$v=19$m=65536,t=3,p=4$c2FsdHNhbHQ$aGFzaGhhc2g"},
		{name: "unsupported version", encoded: "$argon2id$v=1$m=65536,t=3,p=4$c2FsdHNhbHQ$aGFzaGhhc2g"},
		{name: "missing segments", encoded: "$argon2id$v=19$m=65536,t=3,p=4"},
		{name: "malformed params segment", encoded: "$argon2id$v=19$not-params$c2FsdHNhbHQ$aGFzaGhhc2g"},
		{name: "invalid base64 salt", encoded: "$argon2id$v=19$m=65536,t=3,p=4$!!!not-base64!!!$aGFzaGhhc2g"},
		{name: "invalid base64 hash", encoded: "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHQ$!!!not-base64!!!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := auth.VerifyPassword("anything", tt.encoded)
			if err == nil {
				t.Fatalf("VerifyPassword(%q) error = nil, want a decode error", tt.encoded)
			}
			if ok {
				t.Fatalf("VerifyPassword(%q) = true, want false alongside a decode error", tt.encoded)
			}
		})
	}
}

func TestVerifyPassword_IncompatibleVersionIsDistinguishable(t *testing.T) {
	const encoded = "$argon2id$v=1$m=65536,t=3,p=4$c2FsdHNhbHQ$aGFzaGhhc2g"

	_, err := auth.VerifyPassword("anything", encoded)
	if !errors.Is(err, auth.ErrIncompatibleVersion) {
		t.Fatalf("VerifyPassword() error = %v, want errors.Is(err, ErrIncompatibleVersion)", err)
	}
}

func TestVerifyPassword_InvalidHashIsDistinguishable(t *testing.T) {
	_, err := auth.VerifyPassword("anything", "not-a-hash")
	if !errors.Is(err, auth.ErrInvalidHash) {
		t.Fatalf("VerifyPassword() error = %v, want errors.Is(err, ErrInvalidHash)", err)
	}
}
