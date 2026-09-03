package ports

import (
	"context"
	"time"
)

// SeededUserID is the fixed, well-known UUID of the single user M1 seeds on
// an empty database (ADR-0006 and ADR-0007). There is no registration, no
// login, and no second user yet — but every repository method takes an
// actor and filters on it, and this is the ActorID every surface uses until
// M2 builds real authentication.
//
// The first migration (internal/adapters/sqlite/migrations) inserts exactly
// this value; internal/adapters/sqlite's TestMigrateUp_SeedsUser pins the
// two together so they can never drift silently.
const SeededUserID = "00000000-0000-0000-0000-000000000001"

// User is a user account's server-side record. Identity is deliberately
// minimal — there is no username or email column, because M1 and M2 both
// ship with no registration (ADR-0006: "no roles beyond owner... [features]
// become an issue if a real deployment needs it") and exactly one user
// exists, SeededUserID. PasswordHash is nil for a user who has never had a
// password set — the M1 seeded user predates auth entirely, so a fresh
// database has a password-less row until a first-run or CLI password set
// (issue #55) gives it one.
type User struct {
	// ID identifies this user, matching the row's users.id.
	ID string
	// PasswordHash is auth.HashPassword's self-describing PHC-encoded
	// Argon2id hash, or nil if no password has been set yet.
	PasswordHash *string
	// CreatedAt is when this user row was created.
	CreatedAt time.Time
}

// UserRepository persists user accounts. It is intentionally small: M1 and
// M2 add exactly the one capability the application layer's auth use cases
// (issue #55) need — reading a user's credential to verify against, and
// writing a newly hashed one — not a general-purpose user CRUD surface,
// since there is no registration to build one for yet.
//
// Defined here, in the layer that consumes it (ADR-0007) —
// internal/adapters/sqlite implements it, and the application layer depends
// only on this interface.
type UserRepository interface {
	// GetByID returns the user identified by id. Returns a *errs.Error with
	// code NotFound if no such user exists.
	//
	// Unlike every other repository method in this package, this takes no
	// actorID: like SessionRepository.GetByTokenHash and
	// APITokenRepository.GetByTokenHash, establishing who the actor is is
	// exactly what a login flow uses this for — there is no actor to filter
	// on yet.
	GetByID(ctx context.Context, id string) (User, error)

	// SetPasswordHash overwrites the stored password hash for the user
	// identified by actorID with passwordHash — an already-computed
	// auth.HashPassword result; this method does no hashing itself. Returns
	// a *errs.Error with code NotFound if no such user exists.
	SetPasswordHash(ctx context.Context, actorID, passwordHash string) error
}
