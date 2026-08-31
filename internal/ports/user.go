package ports

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
