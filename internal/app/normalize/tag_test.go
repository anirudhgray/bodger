package normalize_test

import (
	"errors"
	"testing"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func TestTag_HashAndPlainProduceTheSameTag(t *testing.T) {
	withHash, err := normalize.Tag("#Reimbursable")
	if err != nil {
		t.Fatalf("Tag(\"#Reimbursable\") unexpected error: %v", err)
	}
	plain, err := normalize.Tag("reimbursable")
	if err != nil {
		t.Fatalf("Tag(\"reimbursable\") unexpected error: %v", err)
	}
	if withHash.String() != plain.String() {
		t.Errorf("Tag(\"#Reimbursable\") = %q, Tag(\"reimbursable\") = %q, want them equal", withHash.String(), plain.String())
	}
	if withHash.String() != "reimbursable" {
		t.Errorf("Tag(\"#Reimbursable\") = %q, want %q", withHash.String(), "reimbursable")
	}
}

func TestTag_InvalidInputIsANamedError(t *testing.T) {
	_, err := normalize.Tag("   ")
	if err == nil {
		t.Fatal("Tag with blank input returned no error")
	}
	var appErr *errs.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not a *errs.Error: %v", err)
	}
	if appErr.Code != errs.InvalidInput {
		t.Errorf("error code = %s, want %s", appErr.Code, errs.InvalidInput)
	}
}
