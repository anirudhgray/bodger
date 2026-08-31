-- +goose Up

-- Instance-wide reference data (data-model.md §3). Referenced by
-- accounts.currency and postings.currency so the foreign_keys=ON pragma
-- (ADR-0007) can enforce that nothing stores a currency code we don't know
-- the minor-unit exponent for.
--
-- This is the same ~20-currency seed as internal/domain/money/currency.go's
-- seedCurrencies — kept in sync by TestCurrencies_MatchDomainSeed, since
-- there is no single source both Go and SQL can read directly.
CREATE TABLE currencies (
    code                TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    symbol              TEXT NOT NULL,
    minor_unit_exponent INTEGER NOT NULL
);

INSERT INTO currencies (code, name, symbol, minor_unit_exponent) VALUES
    ('USD', 'US Dollar', '$', 2),
    ('EUR', 'Euro', '€', 2),
    ('GBP', 'British Pound', '£', 2),
    ('JPY', 'Japanese Yen', '¥', 0),
    ('INR', 'Indian Rupee', '₹', 2),
    ('AUD', 'Australian Dollar', '$', 2),
    ('CAD', 'Canadian Dollar', '$', 2),
    ('CHF', 'Swiss Franc', 'CHF', 2),
    ('CNY', 'Chinese Yuan', '¥', 2),
    ('HKD', 'Hong Kong Dollar', '$', 2),
    ('SGD', 'Singapore Dollar', '$', 2),
    ('NZD', 'New Zealand Dollar', '$', 2),
    ('ZAR', 'South African Rand', 'R', 2),
    ('SEK', 'Swedish Krona', 'kr', 2),
    ('NOK', 'Norwegian Krone', 'kr', 2),
    ('MXN', 'Mexican Peso', '$', 2),
    ('BRL', 'Brazilian Real', 'R$', 2),
    ('KRW', 'South Korean Won', '₩', 0),
    ('BHD', 'Bahraini Dinar', 'BD', 3),
    ('KWD', 'Kuwaiti Dinar', 'KD', 3);

-- +goose Down
DROP TABLE currencies;
