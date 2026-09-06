package sqlite

import (
	"context"

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
