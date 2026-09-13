package app

import "context"

// WhoAmIQuery has no fields beyond ActorID: it exists so the mcp
// surface's trivial read-tier "whoami" tool (issue #259) still maps
// one-to-one onto a real application-layer method (docs/architecture.md
// §3: "MCP tools map one-to-one onto app-layer methods"), the same as
// every other tool this milestone adds, rather than the mcp surface
// reading ports.SeededUserID and calling it a day.
type WhoAmIQuery struct {
	ActorID string
}

// WhoAmIResult reports the identity WhoAmI resolved.
type WhoAmIResult struct {
	ActorID string
}

// WhoAmI resolves the calling actor's own identity — the one thing an
// MCP client can ask before it knows anything else about the ledger it's
// connected to. It looks the actor up via Users.GetByID rather than
// simply echoing ActorID back, so a stale or invalid actor fails the same
// way any other use case would, instead of silently reporting an identity
// that doesn't actually exist.
func (s *Service) WhoAmI(ctx context.Context, q WhoAmIQuery) (WhoAmIResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return WhoAmIResult{}, err
	}
	user, err := s.Users.GetByID(ctx, q.ActorID)
	if err != nil {
		return WhoAmIResult{}, err
	}
	return WhoAmIResult{ActorID: user.ID}, nil
}
