package normalize

import (
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// Currency resolves the ISO 4217 currency code to use for an entry,
// walking ADR-0004's precedence ladder: entry -> account -> user ->
// instance. Each parameter is the value already known at that level, or ""
// if that level has no opinion; the first non-empty level wins. The
// resolved code is validated against money's reference data, so a caller
// never gets back a code that Amount or money.NewMoney would then reject.
//
// instance should in practice never be empty — internal/platform/config
// always resolves a DefaultCurrency — but Currency doesn't trust that: an
// all-empty ladder is a named error, not a panic or a zero-value string.
func Currency(entry, account, user, instance string) (string, error) {
	for _, level := range [...]string{entry, account, user, instance} {
		if level == "" {
			continue
		}
		if _, ok := money.LookupCurrency(level); !ok {
			return "", errs.New(errs.InvalidInput).
				Explain("%q is not a known currency", level).
				Field("currency")
		}
		return level, nil
	}
	return "", errs.New(errs.InvalidInput).
		Explain("no currency could be resolved for this entry").
		Field("currency")
}
