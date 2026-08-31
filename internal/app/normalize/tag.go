package normalize

import (
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// Tag normalises raw into a domain Tag by delegating to ledger.NewTag
// (issue #2): lowercase, strip a leading "#", kebab-case. The actual
// normalisation rule lives on that constructor — Tag's rules are pure and
// need no repository lookup or clock, so ledger.NewTag could technically
// be called directly from a use-case. This wrapper exists anyway, so that
// "every command field is normalised via internal/app/normalize" (ADR-0005's
// one uniform rule) has no silent exception, and so a caller that already
// imports this package for everything else doesn't need to reach into
// internal/domain/ledger just for this one field.
func Tag(raw string) (ledger.Tag, error) {
	t, err := ledger.NewTag(raw)
	if err != nil {
		return ledger.Tag{}, errs.New(errs.InvalidInput).
			Explain("%q isn't a usable tag", raw).
			Field("tags").
			Wrap(err)
	}
	return t, nil
}
