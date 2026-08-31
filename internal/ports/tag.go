package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
)

// TagRepository reads the Tags actorID has ever used. Tags themselves are
// written as a side effect of TransactionRepository.Create and Update
// (data-model.md §6: a tag only exists by being attached to a transaction),
// so this interface is read-only — its purpose is listing the set for
// autocomplete and management surfaces, not independent tag CRUD.
type TagRepository interface {
	// List returns every distinct tag actorID has used, in value order.
	List(ctx context.Context, actorID string) ([]ledger.Tag, error)
}
