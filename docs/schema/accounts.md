# accounts

## Description

<details>
<summary><strong>Table Definition</strong></summary>

```sql
CREATE TABLE "accounts" (
    id                     TEXT PRIMARY KEY,
    user_id                TEXT NOT NULL REFERENCES users (id),
    name                   TEXT NOT NULL,
    kind                   TEXT NOT NULL CHECK (kind IN ('bank', 'cash', 'credit_card', 'wallet', 'investment', 'loan', 'other')),
    currency               TEXT NOT NULL REFERENCES currencies (code),
    institution            TEXT,
    opening_balance_minor  INTEGER NOT NULL DEFAULT 0,
    opening_balance_date   TEXT,
    archived_at            TEXT,
    sort_order             INTEGER NOT NULL DEFAULT 0,
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL,
    UNIQUE (user_id, name)
)
```

</details>

## Columns

| Name | Type | Default | Nullable | Children | Parents | Comment |
| ---- | ---- | ------- | -------- | -------- | ------- | ------- |
| id | TEXT |  | true | [postings](postings.md) [import_batch](import_batch.md) [import_record](import_record.md) [recurring_rules](recurring_rules.md) |  |  |
| user_id | TEXT |  | false |  | [users](users.md) |  |
| name | TEXT |  | false |  |  |  |
| kind | TEXT |  | false |  |  |  |
| currency | TEXT |  | false |  | [currencies](currencies.md) |  |
| institution | TEXT |  | true |  |  |  |
| opening_balance_minor | INTEGER | 0 | false |  |  |  |
| opening_balance_date | TEXT |  | true |  |  |  |
| archived_at | TEXT |  | true |  |  |  |
| sort_order | INTEGER | 0 | false |  |  |  |
| created_at | TEXT |  | false |  |  |  |
| updated_at | TEXT |  | false |  |  |  |

## Constraints

| Name | Type | Definition |
| ---- | ---- | ---------- |
| id | PRIMARY KEY | PRIMARY KEY (id) |
| - (Foreign key ID: 0) | FOREIGN KEY | FOREIGN KEY (currency) REFERENCES currencies (code) ON UPDATE NO ACTION ON DELETE NO ACTION MATCH NONE |
| - (Foreign key ID: 1) | FOREIGN KEY | FOREIGN KEY (user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE NO ACTION MATCH NONE |
| sqlite_autoindex_accounts_2 | UNIQUE | UNIQUE (user_id, name) |
| sqlite_autoindex_accounts_1 | PRIMARY KEY | PRIMARY KEY (id) |
| - | CHECK | CHECK (kind IN ('bank', 'cash', 'credit_card', 'wallet', 'investment', 'loan', 'other')) |

## Indexes

| Name | Definition |
| ---- | ---------- |
| idx_accounts_user_id | CREATE INDEX idx_accounts_user_id ON accounts (user_id) |
| sqlite_autoindex_accounts_2 | UNIQUE (user_id, name) |
| sqlite_autoindex_accounts_1 | PRIMARY KEY (id) |

## Relations

```mermaid
erDiagram

"postings" }o--|| "accounts" : "FOREIGN KEY (account_id) REFERENCES accounts (id) ON UPDATE NO ACTION ON DELETE NO ACTION MATCH NONE"
"import_batch" }o--|| "accounts" : "FOREIGN KEY (target_account_id) REFERENCES accounts (id) ON UPDATE NO ACTION ON DELETE NO ACTION MATCH NONE"
"import_record" }o--o| "accounts" : "FOREIGN KEY (resolved_account_id) REFERENCES accounts (id) ON UPDATE NO ACTION ON DELETE NO ACTION MATCH NONE"
"recurring_rules" }o--|| "accounts" : "FOREIGN KEY (account_id) REFERENCES accounts (id) ON UPDATE NO ACTION ON DELETE NO ACTION MATCH NONE"
"accounts" }o--|| "users" : "FOREIGN KEY (user_id) REFERENCES users (id) ON UPDATE NO ACTION ON DELETE NO ACTION MATCH NONE"
"accounts" }o--|| "currencies" : "FOREIGN KEY (currency) REFERENCES currencies (code) ON UPDATE NO ACTION ON DELETE NO ACTION MATCH NONE"

"accounts" {
  TEXT id PK
  TEXT user_id FK
  TEXT name
  TEXT kind
  TEXT currency FK
  TEXT institution
  INTEGER opening_balance_minor
  TEXT opening_balance_date
  TEXT archived_at
  INTEGER sort_order
  TEXT created_at
  TEXT updated_at
}
"postings" {
  TEXT id PK
  TEXT transaction_id FK
  TEXT account_id FK
  INTEGER amount_minor
  TEXT currency FK
  TEXT category_id FK
  INTEGER sort_order
}
"import_batch" {
  TEXT id PK
  TEXT user_id FK
  TEXT source_format
  TEXT filename
  TEXT file_hash
  TEXT target_account_id FK
  TEXT status
  TEXT created_at
  TEXT updated_at
}
"import_record" {
  TEXT id PK
  TEXT import_batch_id FK
  TEXT user_id FK
  TEXT raw_payload
  TEXT booked_date
  TEXT posted_date
  TEXT description
  INTEGER amount_minor
  TEXT currency FK
  TEXT external_id
  TEXT resolved_account_id FK
  TEXT resolved_category_id FK
  TEXT duplicate_tier
  TEXT duplicate_matched_transaction_id FK
  TEXT duplicate_resolution
  TEXT status
  TEXT transaction_id FK
  INTEGER sort_order
  TEXT created_at
  TEXT updated_at
  TEXT transfer_candidate_record_id FK
  TEXT matched_occurrence_id FK
  TEXT matched_occurrence_resolution
}
"recurring_rules" {
  TEXT id PK
  TEXT user_id FK
  TEXT account_id FK
  TEXT category_id FK
  INTEGER amount_minor
  TEXT description
  TEXT frequency
  INTEGER interval_count
  INTEGER weekday
  INTEGER day_of_month
  INTEGER month_of_year
  TEXT starts_on
  TEXT ends_on
  TEXT archived_at
  TEXT created_at
  TEXT updated_at
}
"users" {
  TEXT id PK
  TEXT created_at
  TEXT password_hash
  TEXT reporting_currency
}
"currencies" {
  TEXT code PK
  TEXT name
  TEXT symbol
  INTEGER minor_unit_exponent
}
```

---

> Generated by [tbls](https://github.com/k1LoW/tbls)
