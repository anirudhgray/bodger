// Package auth provides pure, no-I/O cryptographic primitives for
// bodger's authentication (ADR-0006): Argon2id password hashing behind a
// self-describing PHC-string encoding, and opaque random token
// generation/hashing for session cookies and API tokens.
//
// Nothing here touches a database, a clock, or config — no repository, no
// use-case logic, no HTTP or CLI surface. This is the foundation the
// persistence, application, and surface layers build login and token
// issuance on in later issues.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2idIdentifier is the PHC "function" field this package writes and
// requires on decode. Argon2 also defines argon2i and argon2d variants;
// bodger only ever produces and accepts argon2id (RFC 9106 §4's blanket
// recommendation for password hashing).
const argon2idIdentifier = "argon2id"

// Argon2Params are the Argon2id cost parameters used to hash a password.
// They are recorded in the PHC-encoded hash itself, so a stored hash is
// self-describing: verifying it never needs the caller to remember or
// look up which parameters produced it, and the parameters can change for
// newly hashed passwords without a data migration for existing ones.
type Argon2Params struct {
	// Memory is the amount of memory used, in kibibytes.
	Memory uint32
	// Iterations is the number of passes over the memory.
	Iterations uint32
	// Parallelism is the number of parallel lanes (threads).
	Parallelism uint8
	// SaltLength is the length of the random salt generated per hash, in
	// bytes.
	SaltLength uint32
	// KeyLength is the length of the derived key (the hash itself), in
	// bytes.
	KeyLength uint32
}

// DefaultArgon2Params are bodger's default Argon2id parameters: RFC
// 9106 §4's second recommended option (m=64 MiB, t=3, p=4), the set the
// RFC itself describes for "environments that cannot afford" its
// 2 GiB/p=4 first option — which is exactly ADR-0006's "defaults tuned
// for a small server rather than a workstation" rather than a
// workstation with memory to spare. Making these the default, rather
// than hard-coding them into every call, is what lets a later config
// surface override them (out of scope for this package) without a
// signature change here.
var DefaultArgon2Params = Argon2Params{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 4,
	SaltLength:  16,
	KeyLength:   32,
}

// ErrInvalidHash is returned by VerifyPassword when the encoded hash is
// not a well-formed argon2id PHC string.
var ErrInvalidHash = errors.New("auth: invalid encoded hash")

// ErrIncompatibleVersion is returned by VerifyPassword when the encoded
// hash names an Argon2 version this package's golang.org/x/crypto/argon2
// dependency cannot reproduce.
var ErrIncompatibleVersion = errors.New("auth: incompatible argon2 version")

// argon2Hash is a decoded PHC-string argon2id hash: the parameters it was
// produced with, plus its salt and derived key.
type argon2Hash struct {
	params Argon2Params
	salt   []byte
	hash   []byte
}

// HashPassword derives an Argon2id hash of password under params, with a
// freshly generated random salt, and returns it encoded as a
// self-describing PHC string:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt-base64>$<hash-base64>
//
// Pass DefaultArgon2Params unless a caller has a specific reason to
// override the cost parameters.
func HashPassword(password string, params Argon2Params) (string, error) {
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)

	return encodeArgon2Hash(argon2Hash{params: params, salt: salt, hash: hash}), nil
}

// VerifyPassword reports whether password matches the PHC-encoded Argon2id
// hash previously returned by HashPassword. The parameters used to verify
// are read from encoded itself, not from a caller-supplied default, so a
// hash produced under old parameters still verifies correctly after
// DefaultArgon2Params changes.
//
// It returns (false, nil) for a well-formed hash that simply does not
// match, and (false, non-nil error) — ErrInvalidHash,
// ErrIncompatibleVersion, or a wrapped decode failure — when encoded is
// not a hash this package can evaluate at all.
func VerifyPassword(password, encoded string) (bool, error) {
	h, err := decodeArgon2Hash(encoded)
	if err != nil {
		return false, err
	}

	computed := argon2.IDKey([]byte(password), h.salt, h.params.Iterations, h.params.Memory, h.params.Parallelism, h.params.KeyLength)

	return subtle.ConstantTimeCompare(h.hash, computed) == 1, nil
}

// encodeArgon2Hash renders h as a PHC string.
func encodeArgon2Hash(h argon2Hash) string {
	return fmt.Sprintf(
		"$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2idIdentifier,
		argon2.Version,
		h.params.Memory, h.params.Iterations, h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(h.salt),
		base64.RawStdEncoding.EncodeToString(h.hash),
	)
}

// decodeArgon2Hash parses a PHC string produced by encodeArgon2Hash back
// into its parameters, salt, and hash, validating the algorithm
// identifier, version, and every field's shape along the way.
func decodeArgon2Hash(encoded string) (argon2Hash, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" {
		return argon2Hash{}, ErrInvalidHash
	}

	if parts[1] != argon2idIdentifier {
		return argon2Hash{}, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return argon2Hash{}, fmt.Errorf("%w: %w", ErrInvalidHash, err)
	}
	if version != argon2.Version {
		return argon2Hash{}, ErrIncompatibleVersion
	}

	var params Argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.Memory, &params.Iterations, &params.Parallelism); err != nil {
		return argon2Hash{}, fmt.Errorf("%w: %w", ErrInvalidHash, err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argon2Hash{}, fmt.Errorf("%w: salt: %w", ErrInvalidHash, err)
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argon2Hash{}, fmt.Errorf("%w: hash: %w", ErrInvalidHash, err)
	}

	params.SaltLength = uint32(len(salt))
	params.KeyLength = uint32(len(hash))

	return argon2Hash{params: params, salt: salt, hash: hash}, nil
}
