# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
See [`docs/releasing.md`](docs/releasing.md) for how this file is kept in
sync with each GitHub Release.

## [Unreleased]

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
