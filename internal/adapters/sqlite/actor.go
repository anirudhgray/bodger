package sqlite

import "github.com/anirudhgray/bodger/internal/platform/errs"

// requireActor validates the actor/ownership pair every repository method
// takes (ADR-0006): actorID must be non-empty, and — on a write — must
// match the entity's own user_id. A mismatch is refused rather than
// silently written under a different owner than the one the entity
// declares, which would otherwise let a bug write one user's data under
// another's actor without anyone noticing.
func requireActor(actorID, entityUserID string) *errs.Error {
	if actorID == "" {
		return errs.New(errs.InvalidInput).Explain("An actor is required.").Field("actor_id")
	}
	if entityUserID != "" && entityUserID != actorID {
		return errs.New(errs.NotAllowed).Explain("You may not act on another user's data.")
	}
	return nil
}

// requireActorID validates actorID alone, for reads that don't carry a
// candidate entity to cross-check against.
func requireActorID(actorID string) *errs.Error {
	if actorID == "" {
		return errs.New(errs.InvalidInput).Explain("An actor is required.").Field("actor_id")
	}
	return nil
}
