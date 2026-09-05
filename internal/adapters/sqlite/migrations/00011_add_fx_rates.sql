-- +goose Up

-- M4 schema foundation (issue #128, ADR-0004, data-model.md §8): none of
-- this existed before now — no fx_rates table, no way to record what a
-- cross-currency transfer's implied rate was, no per-user reporting
-- currency (internal/app/accounts.go's "no user rung yet"). Repository
-- code and application logic land in later, separate issues (#130 and
-- friends); this migration is schema only.

-- ADR-0004: "FX rates are a durable record, not a cache." Rows are never
-- evicted and there is no TTL/cleanup job — deliberately. Evicting a rate
-- would silently change a historical report computed under the
-- transaction_date policy, and the whole point of storing rates at all is
-- that a report run today and re-run in five years must produce the same
-- numbers, whether or not the provider still exists. rate is stored as
-- TEXT (a decimal(24,12) string), never REAL: this is financial data, and
-- floats are not an option anywhere in this system (ADR-0004's "money is
-- an integer plus a currency code" applies to fixed-point FX rates too,
-- just via text rather than integer minor units, since sub-minor-unit
-- precision is required here). source only ever holds a provider id in
-- practice — there is no manual-entry path — but it is left as plain TEXT
-- rather than a CHECK-constrained enum, since that's a documentation
-- nuance, not a schema one; the other place a rate is recorded (source =
-- 'implied', for a cross-currency transfer's derived rate) lives on
-- transactions below, never as a row here.
CREATE TABLE fx_rates (
    base       TEXT NOT NULL,
    quote      TEXT NOT NULL,
    rate_date  TEXT NOT NULL,
    rate       TEXT NOT NULL,
    source     TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    PRIMARY KEY (base, quote, rate_date, source)
);

-- Cross-currency transfers record both legs as authoritative (ADR-0004,
-- ADR-0003) and derive the implied rate from them rather than looking one
-- up — it is never a row in fx_rates. Both columns are nullable and
-- populated only for that case; every other transaction leaves them NULL.
-- fx_rate_source mirrors fx_rates.source's shape (plain TEXT, no CHECK)
-- but in practice only ever holds the literal 'implied' here.
ALTER TABLE transactions ADD COLUMN fx_rate_used TEXT;
ALTER TABLE transactions ADD COLUMN fx_rate_source TEXT;

-- Bottom rung of ADR-0004's currency-precedence ladder (explicit entry
-- currency -> account default -> user reporting currency -> instance
-- default), resolved once in internal/app/normalize and nowhere else.
-- Nullable: no user has one set yet, and there is no sensible synthetic
-- default to backfill (an instance default, if any, lives further down
-- the ladder and belongs to a later issue).
ALTER TABLE users ADD COLUMN reporting_currency TEXT;

-- +goose Down

-- Plain additive changes, reversed in the opposite order. SQLite's
-- ALTER TABLE ... DROP COLUMN (supported by the SQLite version this
-- project embeds via modernc.org/sqlite) handles the two added columns
-- directly, same as 00010's password_hash column — no create-copy-drop-
-- rename dance needed here, unlike 00009, which needed it for a genuine
-- type change rather than a plain column addition.
ALTER TABLE users DROP COLUMN reporting_currency;
ALTER TABLE transactions DROP COLUMN fx_rate_source;
ALTER TABLE transactions DROP COLUMN fx_rate_used;
DROP TABLE fx_rates;
