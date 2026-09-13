# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
See [`docs/releasing.md`](docs/releasing.md) for how this file is kept in
sync with each GitHub Release.

## [Unreleased]

## [0.6.0] - 2026-09-13

### Added

* [3268fc94](https://github.com/anirudhgray/bodger/commit/3268fc94): feat(api,cli): export surface — JSON backup and CSV download (#224) (@anirudhgray)
* [6b30a7ba](https://github.com/anirudhgray/bodger/commit/6b30a7ba): feat(api,cli): import surface — upload, review, commit, rollback (#231) (@anirudhgray)
* [6b5056de](https://github.com/anirudhgray/bodger/commit/6b5056de): feat(api,cli): snapshot restore surface — REST/CLI wiring (#232) (@anirudhgray)
* [a3aa98c3](https://github.com/anirudhgray/bodger/commit/a3aa98c3): feat(app): canonical JSON and CSV export (#223) (@anirudhgray)
* [65b47aed](https://github.com/anirudhgray/bodger/commit/65b47aed): feat(app): canonical JSON snapshot restore (#230) (@anirudhgray)
* [a50dd883](https://github.com/anirudhgray/bodger/commit/a50dd883): feat(app): import commit and rollback (#229) (@anirudhgray)
* [2915fdf4](https://github.com/anirudhgray/bodger/commit/2915fdf4): feat(app): import staging pipeline — CSV parser, mapping, duplicate detection (#225) (@anirudhgray)
* [e276d020](https://github.com/anirudhgray/bodger/commit/e276d020): feat(domain,persistence): import batch and record model (#222) (@anirudhgray)
* [cba52e0a](https://github.com/anirudhgray/bodger/commit/cba52e0a): feat(web): import and export UI (#238) (@anirudhgray)

### Changed

* [38312d12](https://github.com/anirudhgray/bodger/commit/38312d12): perf(web): lazy-load the date picker to shrink the initial bundle (#218) (@anirudhgray)

### Fixed

* [8d4d9bbc](https://github.com/anirudhgray/bodger/commit/8d4d9bbc): fix(api): remove ADR citation from balances route description (#217) (@anirudhgray)
* [f4d49a73](https://github.com/anirudhgray/bodger/commit/f4d49a73): fix(app): insert restored categories in topological order (#240) (@anirudhgray)
* [696d62ea](https://github.com/anirudhgray/bodger/commit/696d62ea): fix(sqlite): clear import history before wiping actor's ledger on restore (#239) (@anirudhgray)

## [0.5.0] - 2026-09-12

### Added

* [1364f052](https://github.com/anirudhgray/bodger/commit/1364f052): feat(api,cli): analytics surfaces — REST endpoints, bodger report, and conformance (#193) (@anirudhgray)
* [705e319e](https://github.com/anirudhgray/bodger/commit/705e319e): feat(app): analytics methods — category breakdown, cash flow, trends, savings rate (#192) (@anirudhgray)
* [cb9357f5](https://github.com/anirudhgray/bodger/commit/cb9357f5): feat(app): extend TransactionFilter to ADR-0009's full shape (#191) (@anirudhgray)
* [31ff677b](https://github.com/anirudhgray/bodger/commit/31ff677b): feat(app,api,cli,web): additional analytics — net worth, top transactions, average size, category trends (#206) (@anirudhgray)
* [5492aa17](https://github.com/anirudhgray/bodger/commit/5492aa17): feat(app,api,cli,web): analytics period-granularity selector (#205) (@anirudhgray)
* [5f0a0d67](https://github.com/anirudhgray/bodger/commit/5f0a0d67): feat(app,api,cli,web): balances screen totals overview (#204) (@anirudhgray)
* [d8c61227](https://github.com/anirudhgray/bodger/commit/d8c61227): feat(web): analytics and charts screen (#197) (@anirudhgray)

### Fixed

* [0bb4ba1f](https://github.com/anirudhgray/bodger/commit/0bb4ba1f): fix(app,api,cli): resolve reporting currency via ADR-0004's ladder in analytics queries (#201) (@anirudhgray)
* [549259a6](https://github.com/anirudhgray/bodger/commit/549259a6): fix(web): fix or suppress oxlint warnings, document the convention (#202) (@anirudhgray)

## [0.4.0] - 2026-09-11

### Added

* [dd7ff898](https://github.com/anirudhgray/bodger/commit/dd7ff898): feat(api): FX and multi-currency REST API surface (#155) (@anirudhgray)
* [bc79046c](https://github.com/anirudhgray/bodger/commit/bc79046c): feat(app): FX provider adapter (Frankfurter) (#148) (@anirudhgray)
* [61644c22](https://github.com/anirudhgray/bodger/commit/61644c22): feat(app): FetchFxRates and ListFxRates use cases (#153) (@anirudhgray)
* [17e1d616](https://github.com/anirudhgray/bodger/commit/17e1d616): feat(app): accept an independent to-leg amount on cross-currency transfers (#162) (@anirudhgray)
* [318465d2](https://github.com/anirudhgray/bodger/commit/318465d2): feat(app): conversion policies + ConvertAmount query (#151) (@anirudhgray)
* [80df9ecc](https://github.com/anirudhgray/bodger/commit/80df9ecc): feat(app): cross-currency RecordTransfer (#152) (@anirudhgray)
* [7f8a0d0c](https://github.com/anirudhgray/bodger/commit/7f8a0d0c): feat(app): per-user reporting currency (#147) (@anirudhgray)
* [55a29a9c](https://github.com/anirudhgray/bodger/commit/55a29a9c): feat(cli): FX and multi-currency CLI surface (#154) (@anirudhgray)
* [26396aab](https://github.com/anirudhgray/bodger/commit/26396aab): feat(domain): cross-currency transfer support + Rate type (#144) (@anirudhgray)
* [00a5e84f](https://github.com/anirudhgray/bodger/commit/00a5e84f): feat(persistence): FxRateRepository over fx_rates (#149) (@anirudhgray)
* [01acdee1](https://github.com/anirudhgray/bodger/commit/01acdee1): feat(persistence): add FX schema migration (fx_rates, transfer rate columns, reporting currency) (#142) (@anirudhgray)
* [b0b04b7c](https://github.com/anirudhgray/bodger/commit/b0b04b7c): feat(web): balances currency/policy view + rate provenance display (#158) (@anirudhgray)
* [01ca8c8a](https://github.com/anirudhgray/bodger/commit/01ca8c8a): feat(web): foreign-currency conversion hint and destination-amount field on transaction entry (#160) (@anirudhgray)
* [b9c43bc9](https://github.com/anirudhgray/bodger/commit/b9c43bc9): feat(web): per-row currency conversion and rate backfill on the transaction list (#168) (@anirudhgray)
* [c06796c1](https://github.com/anirudhgray/bodger/commit/c06796c1): feat(web): settings — currency field on new-account form (#175) (@anirudhgray)
* [c5e9a556](https://github.com/anirudhgray/bodger/commit/c5e9a556): feat(web): settings — reporting currency (#157) (@anirudhgray)
* [d1cef2ac](https://github.com/anirudhgray/bodger/commit/d1cef2ac): feat(web): transaction list — provenance on cross-currency rows (#178) (@anirudhgray)

### Changed

* [318b8c39](https://github.com/anirudhgray/bodger/commit/318b8c39): refactor(web): action failures report via toast, not inline (policy change) (#182) (@anirudhgray)

### Fixed

* [f2ff62e5](https://github.com/anirudhgray/bodger/commit/f2ff62e5): fix(api): report a transfer's from-leg amount under amount, not the to-leg (#167) (@anirudhgray)
* [078c1533](https://github.com/anirudhgray/bodger/commit/078c1533): fix(app): fetch a direct base/quote pair instead of always quoting against reporting currency (#166) (@anirudhgray)
* [2fab4c9c](https://github.com/anirudhgray/bodger/commit/2fab4c9c): fix(web): balances refresh rates against the selected display currency (#176) (@anirudhgray)
* [176584cd](https://github.com/anirudhgray/bodger/commit/176584cd): fix(web): category name staleness and missing currency labels on transaction entry (#181) (@anirudhgray)
* [7a7d3e1d](https://github.com/anirudhgray/bodger/commit/7a7d3e1d): fix(web): reporting currency, balances layout, and lib/settings cleanup (#184) (@anirudhgray)
* [3c1b0b8c](https://github.com/anirudhgray/bodger/commit/3c1b0b8c): fix(web): use default shadcn tabs style for Settings sub-nav (#150) (@anirudhgray)

## [0.3.0] - 2026-09-05

### Added

* [71c43b9f](https://github.com/anirudhgray/bodger/commit/71c43b9f): feat(web): category hierarchy view + quick-create from pickers (#123) (@anirudhgray)
* [966365ff](https://github.com/anirudhgray/bodger/commit/966365ff): feat(web): cross-navigation between related screens (#113) (@anirudhgray)
* [877ac406](https://github.com/anirudhgray/bodger/commit/877ac406): feat(web): design tokens and shadcn primitives for M3 Rivendell (#103) (@anirudhgray)
* [7c336f32](https://github.com/anirudhgray/bodger/commit/7c336f32): feat(web): split Settings into nested subpages (#122) (@anirudhgray)
* [9191783b](https://github.com/anirudhgray/bodger/commit/9191783b): feat(web): toast notifications for post-dialog/row feedback (#114) (@anirudhgray)

### Changed

* [d6bf1bfd](https://github.com/anirudhgray/bodger/commit/d6bf1bfd): refactor(web): audit and align components/ui against the design system (#105) (@anirudhgray)
* [52be9609](https://github.com/anirudhgray/bodger/commit/52be9609): refactor(web): wire a Radix popover+calendar date picker (#104) (#118) (@anirudhgray)

### Fixed

* [a0efd528](https://github.com/anirudhgray/bodger/commit/a0efd528): fix(release): embed real web UI in release binaries, add release-dry-run (#112) (@anirudhgray)
* [76c1eee8](https://github.com/anirudhgray/bodger/commit/76c1eee8): fix(web): responsive layout and sidebar nav (#88) (#121) (@anirudhgray)

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

[Unreleased]: https://github.com/anirudhgray/bodger/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/anirudhgray/bodger/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/anirudhgray/bodger/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/anirudhgray/bodger/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/anirudhgray/bodger/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/anirudhgray/bodger/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/anirudhgray/bodger/releases/tag/v0.1.0
