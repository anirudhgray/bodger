# ADR-0006 — Authentication, authorisation, and the path to multi-user

**Status:** Accepted · 2026-08-31

## Context

The brief (§24) asks for the *minimum sensible* architecture covering single-user deployments, multiple users, authentication, authorisation, and per-user data isolation — and explicitly warns against building elaborate authentication infrastructure before the core product model is understood.

The tension is a familiar one. Building multi-user auth now is exactly the premature infrastructure the brief warns about. Building single-user with no notion of ownership means a painful retrofit later: every table needs a column, every query needs a predicate, and the migration has to invent an owner for existing rows.

There is also a real safety question in M1. If the REST API ships before auth, an unauthenticated financial API exists on someone's network.

## Decision

### Data is shaped for multi-user from day one; the features are not built

Every user-owned table carries `user_id` from the first migration. Every repository method takes an actor and filters on it. Every command and query struct carries `ActorID` ([ADR-0005](0005-shared-application-layer.md)).

In M1 there is exactly one user, created on first run. The plumbing is exercised from the start, so it cannot rot — the alternative, a `user_id` column that is always the same value and never filtered on, is worse than no column, because it looks like isolation without being it.

This costs almost nothing now and removes the expensive part of multi-user later. What remains genuinely unbuilt is the *product* of multi-user: registration, invitations, roles, sharing, joint accounts. Those need product decisions, not schema changes.

### Authorisation lives in the application layer

Not in handlers, not in middleware, not in repositories. The app layer receives `ActorID` and decides. Middleware *authenticates* — it establishes who the actor is — and hands that to the app layer, which decides what they may do.

Four surfaces with authorisation in middleware would mean four authorisation models. Concretely, the MCP server has no HTTP middleware at all, so any rule living there would simply not apply to it.

### Two credential types, one identity model

| Credential | Used by | Mechanism |
| --- | --- | --- |
| **Session cookie** | Web UI | Opaque random token, stored hashed server-side; `HttpOnly`, `Secure`, `SameSite=Lax`; sliding expiry |
| **API token** | CLI (remote mode), scripts, external clients | `bdg_<random>`, stored as a SHA-256 hash; `Authorization: Bearer`; named, listable, revocable, optional expiry |

Server-side sessions rather than JWTs: revocation is a `DELETE`, there is no refresh-token dance, and a self-hosted single-instance deployment gains nothing from stateless tokens. Passwords are hashed with **Argon2id**.

The in-process CLI and MCP server need no credential — they open the database directly and act as the single local user. Filesystem permissions on the database file are the boundary there, and that is the correct boundary for a local tool: someone who can read `bodger.db` can read the ledger regardless of what the application checks.

### M1 ships without auth, and refuses to be unsafe about it

Milestone 1 has no login. It also cannot be exposed by accident:

- The server binds `127.0.0.1` by default.
- **If configured to bind a non-loopback address while no authentication is configured, the server refuses to start**, with an error naming the two ways to resolve it.

This turns "I'll add auth later" from a security risk into a startup error. Auth lands in M2, with the web UI — the milestone that first genuinely needs it.

### Deliberately not built

No OIDC, no OAuth, no SAML, no LDAP, no social login, no email verification, no password reset flow (M2 resets a password via CLI, which is the honest answer for a self-hosted app with no mail server), no roles beyond owner, no sharing, no joint accounts. Each becomes an issue if a real deployment needs it.

## Alternatives considered

**No `user_id` until multi-user is built.** Simplest schema now. Rejected: the retrofit touches every table, every query, and every test, and must invent an owner for existing rows during migration. One column and one predicate per query, exercised from day one, is much cheaper.

**Full multi-user in M1** — registration, roles, invitations. Rejected as precisely the premature infrastructure the brief warns against, and it would delay the ledger core, which is the part that actually needs to be right first.

**JWTs.** Stateless, fashionable, no session table. Rejected: revocation requires a denylist that reintroduces the state JWTs were meant to remove, and a single-instance self-hosted app has no horizontal-scaling problem to solve. An opaque token in a table is simpler and strictly more controllable.

**Basic auth or a single shared static token.** Trivially simple, and adequate for one user behind a VPN. Rejected because it cannot express multiple users or per-client revocation, and it would be replaced almost immediately — a static token in a shell profile cannot be rotated without breaking every script at once.

**Delegate auth to a reverse proxy** (Authelia, oauth2-proxy, Tailscale). Common in self-hosted deployments and genuinely good. Rejected as the *only* mechanism, because it makes a basic install require a second piece of infrastructure. A trusted-header mode for exactly this setup is a reasonable future issue.

**Authorisation in HTTP middleware.** Conventional. Rejected because the MCP server and CLI have no HTTP middleware, so the rules would not apply to them — a hole that would be invisible until an agent read someone else's data.

## Consequences

**Good.** The multi-user migration is a feature, not a rewrite. Authorisation is tested once at the app layer and applies identically to all four surfaces. Revocation is immediate. M1 cannot be accidentally exposed. Local CLI and MCP use stays frictionless.

**Bad:**

- **`user_id` and `ActorID` are threaded everywhere before anything varies.** Noise in M1 for a payoff that arrives later. Accepted deliberately.
- **A session lookup hits the database on every authenticated request.** Irrelevant at this scale; a cache is a measured optimisation if it ever isn't.
- **Filesystem permissions are the only boundary for local CLI and MCP access.** Correct for a local tool, but worth stating plainly rather than implying the application enforces something it does not.
- **No password reset flow in M2.** A user who forgets their password uses the CLI on the host. Acceptable for self-hosted; an issue if it isn't.
- **The refuse-to-start rule will annoy someone** who deliberately runs behind a trusted proxy in M1. The error message names the override, and trusted-header mode is a filed follow-up.
- **Argon2id costs memory per login** — parameters are configurable, defaults tuned for a small server rather than a workstation.
