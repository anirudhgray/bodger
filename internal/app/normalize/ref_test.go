package normalize_test

import (
	"errors"
	"testing"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

var refCandidates = []normalize.Candidate{
	{ID: "11111111-1111-1111-1111-111111111111", Name: "HDFC Savings"},
	{ID: "22222222-2222-2222-2222-222222222222", Name: "hdfc credit card"},
	{ID: "33333333-3333-3333-3333-333333333333", Name: "Cash"},
}

func TestRef_ByUUID(t *testing.T) {
	got, err := normalize.Ref("11111111-1111-1111-1111-111111111111", refCandidates)
	if err != nil {
		t.Fatalf("Ref(uuid) unexpected error: %v", err)
	}
	if got != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("Ref(uuid) = %q, want the matching candidate ID", got)
	}
}

func TestRef_UUIDNotAmongCandidates(t *testing.T) {
	_, err := normalize.Ref("99999999-9999-9999-9999-999999999999", refCandidates)
	if err == nil {
		t.Fatal("Ref with a UUID not in the candidate set returned no error")
	}
	var appErr *errs.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not a *errs.Error: %v", err)
	}
	if appErr.Code != errs.NotFound {
		t.Errorf("error code = %s, want %s", appErr.Code, errs.NotFound)
	}
}

func TestRef_ExactName(t *testing.T) {
	got, err := normalize.Ref("Cash", refCandidates)
	if err != nil {
		t.Fatalf("Ref(exact name) unexpected error: %v", err)
	}
	if got != "33333333-3333-3333-3333-333333333333" {
		t.Errorf("Ref(exact name) = %q, want Cash's ID", got)
	}
}

func TestRef_UniqueCaseInsensitiveMatch(t *testing.T) {
	got, err := normalize.Ref("cash", refCandidates)
	if err != nil {
		t.Fatalf("Ref(case-insensitive) unexpected error: %v", err)
	}
	if got != "33333333-3333-3333-3333-333333333333" {
		t.Errorf("Ref(\"cash\") = %q, want Cash's ID", got)
	}
}

// TestRef_AmbiguousExactCaseInsensitiveCollision covers the disambiguation
// path: Ref matches candidate names case-insensitively as whole strings,
// not by substring, so two accounts that differ only by case collide.
func TestRef_AmbiguousExactCaseInsensitiveCollision(t *testing.T) {
	collidingCandidates := []normalize.Candidate{
		{ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Name: "Groceries"},
		{ID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", Name: "GROCERIES"},
	}

	_, err := normalize.Ref("groceries", collidingCandidates)
	if err == nil {
		t.Fatal("Ref with two case-insensitively colliding names returned no error")
	}
	var appErr *errs.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not a *errs.Error: %v", err)
	}
	if appErr.Code != errs.InvalidInput {
		t.Errorf("error code = %s, want %s", appErr.Code, errs.InvalidInput)
	}

	candidates, ok := appErr.Details["candidates"].([]string)
	if !ok {
		t.Fatalf("Details[\"candidates\"] = %#v, want []string", appErr.Details["candidates"])
	}
	if len(candidates) != 2 {
		t.Fatalf("Details[\"candidates\"] = %v, want both colliding names listed", candidates)
	}
}

func TestRef_NoMatch(t *testing.T) {
	_, err := normalize.Ref("Nonexistent Account", refCandidates)
	if err == nil {
		t.Fatal("Ref with no match returned no error")
	}
	var appErr *errs.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not a *errs.Error: %v", err)
	}
	if appErr.Code != errs.NotFound {
		t.Errorf("error code = %s, want %s", appErr.Code, errs.NotFound)
	}
}

func TestRef_EmptyInput(t *testing.T) {
	_, err := normalize.Ref("   ", refCandidates)
	if err == nil {
		t.Fatal("Ref with blank input returned no error")
	}
	var appErr *errs.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not a *errs.Error: %v", err)
	}
	if appErr.Code != errs.InvalidInput {
		t.Errorf("error code = %s, want %s", appErr.Code, errs.InvalidInput)
	}
}
