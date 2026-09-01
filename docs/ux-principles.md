# UX principles

The standing expectations every surface must meet. [ADR-0010](decisions/0010-personal-finance-not-accounting-software.md) is the decision; this is the checkable version of it.

Written to be **used in review**. Where an expectation can be stated as something a reviewer can verify rather than something they must feel, it is. Where it can't, that's said plainly rather than dressed up as a rule.

Applies to the CLI, REST API, web UI, and MCP server equally. "Users" includes an agent calling an MCP tool — a tool description full of accounting jargon fails this document the same way a form label would.

---

## 1. The user this is for

Someone who wants to know where their money went. They are not an accountant, they have not read the docs, and they are entering a transaction while standing in a shop.

They will happily learn *bodger's* vocabulary — accounts, categories, transfers, budgets. They will not learn *accounting's*.

The five questions the product exists to answer:

> Where did my money go? Where did it come from? What do I have available? How am I doing against my budget? How has that changed over time?

A feature that doesn't eventually serve one of those needs a reason.

---

## 2. Vocabulary

**Banned in every user-facing string** — labels, flags, subcommands, help text, prompts, errors, MCP tool names and descriptions, API field names that a person will read:

| Never say | Say instead |
| --- | --- |
| posting, entry line, journal entry, journal | (nothing — it's internal; describe the effect) |
| ledger | your transactions, your history |
| debit, credit | money in, money out — or just the sign |
| double-entry, balanced entry | (nothing) |
| chart of accounts | your accounts |
| accounting period, fiscal period | month, period, date range |
| reconcile | check against your statement |
| tx, txn | transaction, or the verb (`spend`, `receive`) |
| CRUD, upsert, soft delete | add / edit / delete |
| null, nil, undefined | not set, none, any |
| enum, enum value | (list the actual options) |

Internal code uses the precise term — `Posting` is a `Posting` in `internal/domain`. The boundary is the string a person reads.

**Prefer verbs the user would use.** This is the target register: *"I spent ₹800 on groceries." "My salary came in." "I moved ₹20,000 from savings to checking."*

That makes `spend` / `receive` / `move` the CLI verbs. See §7.

**"Ledger core" and similar are fine internally** — milestone names, package names, this repo's docs. They are not user-facing.

---

## 3. Fast entry

Recording a transaction is the single most frequent thing anyone does here. It gets a budget:

- **Three required inputs** for the common case: amount, category, and account. Everything else has a default or is optional.
- **Date defaults to today**, resolved server-side ([ADR-0005](decisions/0005-shared-application-layer.md)). The user does not type today's date to record something that happened today.
- **Currency defaults** via the precedence ladder ([ADR-0004](decisions/0004-multi-currency-and-fx.md)). A single-currency user should never see a currency field.
- **Account defaults** to the most recently used, or the only one if there's one.
- **One command, one screen, one tool call.** Recording an expense never requires a second step, a confirmation dialog, or a wizard.
- **Input is forgiving.** `1800`, `1,800`, `1800.50`, `₹1800` all work. Category and account match on name, case-insensitively, and an ambiguous match lists the candidates rather than guessing ([ADR-0005](decisions/0005-shared-application-layer.md)).

*Checkable:* a new user with one account can record an expense in one command with three flags, or one screen with three fields.

---

## 4. Progressive disclosure

This is how "simple" and "powerful" coexist, and it's the part most likely to be got wrong in either direction.

**Three layers**, and everything belongs to exactly one:

| Layer | Contains | Rule |
| --- | --- | --- |
| **Common** | Record spend / receive / move, see balances, see this month | Never more than three required inputs. Visible by default |
| **Occasional** | Splits, editing, notes, tags, filters, budgets, import | One deliberate step away — a flag, a disclosure control, an optional argument. Never blocks the common path |
| **Power** | Full filter model, machine-readable output, export, multi-currency policy, MCP | Fully documented, never surfaced in the common path |

**Omitting capability is not simplification.** If the choice is between hiding a feature and removing it, hide it — [ADR-0010](decisions/0010-personal-finance-not-accounting-software.md) consequence 2. A reviewer citing this document to argue *against* a capability has misread it.

**Every layer has an escape hatch.** `--json` on every data-returning CLI command, the full filter model on every list, and a complete export ([ADR-0008](decisions/0008-import-export-architecture.md)). A user who outgrows the interface is never stuck inside it — that is what makes the simple default safe to offer.

---

## 5. Forgiving workflows

- **Everything is editable.** Wrong amount, wrong category, wrong date, wrong account — fix it in place. No reversal entries ([ADR-0002](decisions/0002-authoritative-ledger-and-corrections.md)).
- **Nothing is silently destructive.** Deletes are soft. Import commits are one transaction and roll back as a unit ([ADR-0008](decisions/0008-import-export-architecture.md)).
- **Confirmation is reserved for genuinely destructive, hard-to-undo actions** — rolling back an import, deleting an account with history. Confirming an ordinary expense is friction that trains people to stop reading dialogs.
- **Partial input is preserved.** A validation failure never discards what was typed.
- **The system never quietly decides something significant.** Suspected duplicate imports and detected transfers are *proposed*, never auto-applied ([ADR-0008](decisions/0008-import-export-architecture.md)). Automatic categorisation, if it ever exists, suggests.

---

## 6. Errors, and telling the truth

Errors are the place UX and correctness meet, and the place jargon leaks most. [ADR-0011](decisions/0011-error-model.md) is the machinery; this is what the user should experience.

The split that makes both possible: **domain detail** (which account, which value, which field) always reaches the user, because that's what makes an error actionable. **Infrastructure detail** (the wrapped cause chain, SQL, paths) never does — it goes to the log.

- **Name what failed, which value, and what to do.** "Couldn't find an account called 'hdcf'. Did you mean 'HDFC Savings'?" — not "invalid account reference."
- **No internal references.** No ADR numbers, doc paths, package names, or SQL. This is CLAUDE.md's rule and it applies to errors specifically, which are otherwise allowed to be detailed and specific.
- **Every error is actionable or honestly refers you onward.** If the user can fix it, say how. If they can't, it's a bug — say so plainly and give them the reference that finds it in the log, rather than blaming their input.
- **Never silently wrong.** A missing FX rate fails loudly with a named error rather than falling back to 1.0 or dropping the row ([ADR-0004](decisions/0004-multi-currency-and-fx.md)). A number the user cannot trust is worse than an error they can act on.
- **Every converted amount shows its rate, date, and source** ([ADR-0004](decisions/0004-multi-currency-and-fx.md)). Transparency *is* a UX property here: this is the user's money, and a figure they can't check is a figure they won't believe.
- **A single-currency user must never meet the currency system at all.** No currency field on entry, no conversion policy, no rate, no "converted from" annotation — those appear only once a second currency actually exists in their data. Multi-currency is a first-class capability, not a first-class *presence*: the machinery in [ADR-0004](decisions/0004-multi-currency-and-fx.md) describes the converted case, and nothing in it should surface on the path most users are on.

---

## 7. Applying this to the CLI

Worked through because it's M1's user-facing surface, and because it already went wrong once — issue #7 was specified with `bodger tx out`, which fails §2.

```
bodger spend 800 groceries                    # account defaults, date defaults
bodger spend 800 groceries --account "HDFC Savings" --on 2026-08-14
bodger receive 150000 salary
bodger move 20000 --from Savings --to Checking

bodger balance                                # the "what do I have?" question
bodger accounts | categories
```

- **One vocabulary, not two.** No `bodger tx out` alias alongside `bodger spend`. A second set of verbs doubles the CLI surface, the help text, and the conformance rows, and gives users two ways to do one thing — which §4 argues against. Add an alias if a scripting user actually asks for one.
- Positional amount and category, because that's the order the sentence goes in.
- `--on` rather than `--date`: it reads as English, and `--date today` still works.
- No `--today` flag. `--on today` is handled centrally, and a surface implementing its own would fail CI ([ADR-0005](decisions/0005-shared-application-layer.md)).
- `--json` everywhere. `--split "Groceries:3500" --split "Household:1000"` for the occasional layer.

---

## 8. What this document can't do

**"A joy to use" is not testable, and pretending otherwise would be worse than admitting it.** Everything above is the part that converts into review checks. The rest — whether the tool feels fast, whether the branding is fun, whether a first run is delightful — is a judgement call this document hands to the reviewer rather than resolving.

Two gaps worth naming rather than hiding:

- **Vocabulary is only partly enforced.** §2's table is now checked by `make check` — but only over the error registry's default messages and cobra help and flag usage text ([`contributing.md`](contributing.md#the-vocabulary-check)). Those two have no ambiguity about what "user-facing" means; OpenAPI descriptions, MCP tool descriptions, and web UI strings do, and a check with false positives is one people learn to bypass. Everywhere else, and everything in this document that isn't a word on a list, §2 remains a review checklist.
- **No real user has used this.** Every expectation here is derived from the brief and from reasoning, not from watching anyone. The first genuine usability signal arrives with M2, and this document should be revised against it rather than defended.
