# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
See [`docs/releasing.md`](docs/releasing.md) for how this file is kept in
sync with each GitHub Release.

## [Unreleased]

## [0.2.0] - 2026-09-04

### Added

* aa5a797c: feat(api): session-cookie + bearer-token middleware, auth endpoints (#77) (@anirudhgray)
* 08455440: feat(app): login, logout, password set, and API-token issue/list/revoke use cases (#74) (@anirudhgray)
* 30db3cde: feat(cli): auth commands (set-password, API token create/list/revoke) (#76) (@anirudhgray)
* 4e442abd: feat(cmd): wire up structured logging for error cause chains (#68) (@anirudhgray)
* df09674d: feat(persistence): sessions and api_tokens tables + password_hash on users (#70) (@anirudhgray)
* 2c2eb7b9: feat(platform): add Argon2id password hashing and token primitives (#67) (@anirudhgray)
* 566a1e67: feat(release): credit commit authors, link changelog entries and full history (#52) (@anirudhgray)
* 1ce0f23c: feat(web): React+TS+Vite scaffold, embed pipeline, dev proxy (#69) (@anirudhgray)
* 89741260: feat(web): account balances view (#79) (@anirudhgray)
* 027d8d67: feat(web): fast transaction entry (#80) (@anirudhgray)
* dfe115f8: feat(web): humanize account/category type display copy (#94) (@anirudhgray)
* 0dff3726: feat(web): login and session handling (#78) (@anirudhgray)
* 9dc2d395: feat(web): serve the dev server over HTTPS via vite-plugin-mkcert (#87) (@anirudhgray)
* 03a2783e: feat(web): settings — password, API tokens, accounts/categories CRUD (#82) (@anirudhgray)
* eec6457b: feat(web): transaction list with filters (#81) (@anirudhgray)

### Fixed

* b3cf6a65: fix(release): re-fetch tags before reading tag annotation content (#51) (@anirudhgray)

## [0.1.0] - 2026-09-02

### Added

* 7af13a58: feat(api): REST API v1 and OpenAPI description (#35)
* d5ddec96: feat(api): generate openapi.json from DTOs and validate responses against it (kin-openapi) (#37)
* 234c8542: feat(app): ledger use cases and derived balances (#27)
* 2f6634ac: feat(app): normalize package and service container (#24)
* ac00e916: feat(cli): accept outflow/inflow/transfer as aliases for transactions list --type (#45)
* ae007608: feat(cli): account, category, transaction, and balance commands (#31)
* e80f4c0d: feat(cli): expose transaction listing/editing/deletion and account/category renaming (#38)
* 1f5d50c1: feat(domain): accounts, categories, transactions, and postings (#18)
* e0477190: feat(domain): add institution, sort_order, and archived_at to Account and Category (#26)
* 2f673181: feat(domain): money, currency, and date value objects (#15)
* 3b97dca6: feat(persistence): SQLite adapter, repositories, and migrations (#20)
* c63940e3: feat(platform): injected clock, config, IDs, and shared validation errors (#16)
* 42f03d98: feat: initial commit

### Fixed

* 57094ab5: fix(cli): stop bootstrap failures printing raw errors to the terminal (#42)
* 5ae7d547: fix(infra): make lint fail loudly on an incompatible golangci-lint (#23)
* c76bd7e7: fix(infra): pre-push hook executable bit, worktree hygiene (#14)

[Unreleased]: https://github.com/anirudhgray/bodger/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/anirudhgray/bodger/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/anirudhgray/bodger/releases/tag/v0.1.0
