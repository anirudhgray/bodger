package sqlite

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// FxRateRepository implements ports.FxRateRepository over a *DB.
//
// Unlike this package's other repositories, fx_rates carries no
// actor/user scoping (ADR-0004: a rate is the same fact for every user of
// this instance), so its methods take no actorID and never call
// requireActor/requireActorID.
type FxRateRepository struct {
	db *DB
}

// NewFxRateRepository constructs an FxRateRepository backed by db.
func NewFxRateRepository(db *DB) *FxRateRepository {
	return &FxRateRepository{db: db}
}

var _ ports.FxRateRepository = (*FxRateRepository)(nil)

// Store implements ports.FxRateRepository.
func (r *FxRateRepository) Store(ctx context.Context, rate fx.Rate, date domain.Date, source string) error {
	if err := requireRateKey(rate, source); err != nil {
		return err
	}
	now := formatTime(r.db.clock.Now())
	if err := upsertFxRate(ctx, r.db.write, rate, date, source, now); err != nil {
		return err
	}
	return nil
}

// StoreBatch implements ports.FxRateRepository.
func (r *FxRateRepository) StoreBatch(ctx context.Context, rows []ports.FxRateRow) error {
	if len(rows) == 0 {
		return nil
	}
	for _, row := range rows {
		if err := requireRateKey(row.Rate, row.Source); err != nil {
			return err
		}
	}

	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	now := formatTime(r.db.clock.Now())
	for _, row := range rows {
		if err := upsertFxRate(ctx, tx, row.Rate, row.Date, row.Source, now); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return nil
}

// Lookup implements ports.FxRateRepository. It loads every stored
// candidate for (base, quote) up to and including date, then hands them
// to fx.SelectRate — the pure selection logic itself is never
// reimplemented here in SQL.
//
// A pair can have rows from more than one source (fx_rates' primary key
// includes source); Lookup doesn't filter by source, so all of them
// become candidates. In practice there is currently only one provider
// configured per instance, so this doesn't yet matter; a future
// source-preference rule (if one is ever needed) belongs in fx.SelectRate
// or a wrapper around it, not as ad hoc filtering here.
func (r *FxRateRepository) Lookup(ctx context.Context, base, quote string, date domain.Date, windowDays int) (fx.Selection, error) {
	if base == "" || quote == "" {
		return fx.Selection{}, errs.New(errs.InvalidInput).Explain("Both a base and a quote currency are required.").Field("base")
	}

	rows, err := r.db.read.QueryContext(ctx, `
		SELECT rate_date, rate, source FROM fx_rates
		WHERE base = ? AND quote = ? AND rate_date <= ?
	`, base, quote, formatDate(date))
	if err != nil {
		return fx.Selection{}, errs.New(errs.Internal).Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	var candidates []fx.RateCandidate
	for rows.Next() {
		var dateCol, rateCol, sourceCol string
		if err := rows.Scan(&dateCol, &rateCol, &sourceCol); err != nil {
			return fx.Selection{}, errs.New(errs.Internal).Wrap(err)
		}
		d, err := parseDate(dateCol)
		if err != nil {
			return fx.Selection{}, errs.New(errs.Internal).Wrap(err)
		}
		value, err := decimal.NewFromString(rateCol)
		if err != nil {
			return fx.Selection{}, errs.New(errs.Internal).Wrap(err)
		}
		rate, err := fx.NewRate(base, quote, value)
		if err != nil {
			return fx.Selection{}, errs.New(errs.Internal).Wrap(err)
		}
		candidates = append(candidates, fx.RateCandidate{Date: d, Rate: rate, Source: sourceCol})
	}
	if err := rows.Err(); err != nil {
		return fx.Selection{}, errs.New(errs.Internal).Wrap(err)
	}

	sel, err := fx.SelectRate(candidates, date, windowDays)
	if err != nil {
		switch {
		case errors.Is(err, fx.ErrNoRateWithinWindow):
			return fx.Selection{}, errs.New(errs.NotFound).
				Explain("No %s/%s rate available for %s within %d day(s).", base, quote, date.String(), windowDays).
				Wrap(err)
		case errors.Is(err, fx.ErrInvalidStalenessWindow):
			return fx.Selection{}, errs.New(errs.InvalidInput).
				Explain("The staleness window must not be negative.").
				Field("window_days").
				Wrap(err)
		default:
			return fx.Selection{}, errs.New(errs.Internal).Wrap(err)
		}
	}
	return sel, nil
}

// InUsePairs implements ports.FxRateRepository. It unions every distinct
// currency actorID's accounts declare (accounts.currency) with every
// distinct currency actorID's non-deleted transactions' postings actually
// record (postings.currency, via a join to transactions for the
// deleted_at/user_id filter — postings themselves carry no user_id) —
// "every currency actually used", not just each account's own default —
// and pairs each one that isn't already reportingCurrency against it.
func (r *FxRateRepository) InUsePairs(ctx context.Context, actorID, reportingCurrency string) ([]ports.CurrencyPair, error) {
	if err := requireActorID(actorID); err != nil {
		return nil, err
	}
	if reportingCurrency == "" {
		return nil, errs.New(errs.InvalidInput).Explain("A reporting currency is required.").Field("reporting_currency")
	}

	rows, err := r.db.read.QueryContext(ctx, `
		SELECT DISTINCT currency FROM (
			SELECT currency FROM accounts WHERE user_id = ?
			UNION
			SELECT p.currency
			FROM postings p
			JOIN transactions t ON t.id = p.transaction_id
			WHERE t.user_id = ? AND t.deleted_at IS NULL
		)
		WHERE currency != ?
		ORDER BY currency
	`, actorID, actorID, reportingCurrency)
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	var pairs []ports.CurrencyPair
	for rows.Next() {
		var currency string
		if err := rows.Scan(&currency); err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		pairs = append(pairs, ports.CurrencyPair{Base: currency, Quote: reportingCurrency})
	}
	if err := rows.Err(); err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	return pairs, nil
}

// requireRateKey validates the fields that make up fx_rates' primary key
// (base, quote, rate_date, source) before a write: an empty base/quote
// can only happen via a zero-value fx.Rate{} constructed outside the fx
// package's validating constructors, and source has no other validation
// point since it's a plain caller-supplied string.
func requireRateKey(rate fx.Rate, source string) *errs.Error {
	if rate.Base() == "" || rate.Quote() == "" {
		return errs.New(errs.InvalidInput).Explain("A rate must have both a base and a quote currency.").Field("rate")
	}
	if source == "" {
		return errs.New(errs.InvalidInput).Explain("A rate source is required.").Field("source")
	}
	return nil
}

// upsertFxRate writes one fx_rates row through ex — either r.db.write
// directly (Store) or a transaction (StoreBatch) — replacing any existing
// row for the same (base, quote, rate_date, source) key rather than
// conflicting on it: re-fetching an already-stored day is expected to
// converge on the provider's current answer, not accumulate duplicate
// rows. fetchedAt is a formatTime-rendered timestamp — an audit column
// stamped from the repository's own clock, the same way created_at/
// updated_at are elsewhere in this package, not a domain-visible value the
// caller resolves.
func upsertFxRate(ctx context.Context, ex execer, rate fx.Rate, date domain.Date, source, fetchedAt string) *errs.Error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO fx_rates (base, quote, rate_date, rate, source, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (base, quote, rate_date, source) DO UPDATE SET
			rate = excluded.rate,
			fetched_at = excluded.fetched_at
	`, rate.Base(), rate.Quote(), formatDate(date), rate.String(), source, fetchedAt)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't store the %s/%s rate for %s.", rate.Base(), rate.Quote(), date.String())
	}
	return nil
}
