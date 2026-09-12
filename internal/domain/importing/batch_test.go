package importing_test

import (
	"errors"
	"testing"

	"github.com/anirudhgray/bodger/internal/domain/importing"
)

func mustBatch(t *testing.T) importing.ImportBatch {
	t.Helper()
	b, err := importing.NewImportBatch("batch-1", "user-1", "csv", "statement.csv", "sha256:abc", "acc-1")
	if err != nil {
		t.Fatalf("NewImportBatch(...) = %v, want success", err)
	}
	return b
}

func TestNewImportBatch(t *testing.T) {
	t.Parallel()

	t.Run("starts staged", func(t *testing.T) {
		t.Parallel()
		b := mustBatch(t)
		if b.Status() != importing.ImportBatchStatusStaged {
			t.Errorf("Status() = %q, want %q", b.Status(), importing.ImportBatchStatusStaged)
		}
	})

	t.Run("round-trips its fields", func(t *testing.T) {
		t.Parallel()
		b, err := importing.NewImportBatch("batch-1", "user-1", "csv", "statement.csv", "sha256:abc", "acc-1")
		if err != nil {
			t.Fatalf("NewImportBatch(...) = %v, want success", err)
		}
		if b.ID() != "batch-1" {
			t.Errorf("ID() = %q, want %q", b.ID(), "batch-1")
		}
		if b.UserID() != "user-1" {
			t.Errorf("UserID() = %q, want %q", b.UserID(), "user-1")
		}
		if b.SourceFormat() != "csv" {
			t.Errorf("SourceFormat() = %q, want %q", b.SourceFormat(), "csv")
		}
		if b.Filename() != "statement.csv" {
			t.Errorf("Filename() = %q, want %q", b.Filename(), "statement.csv")
		}
		if b.FileHash() != "sha256:abc" {
			t.Errorf("FileHash() = %q, want %q", b.FileHash(), "sha256:abc")
		}
		if b.TargetAccountID() != "acc-1" {
			t.Errorf("TargetAccountID() = %q, want %q", b.TargetAccountID(), "acc-1")
		}
	})

	for _, tt := range []struct {
		name            string
		id              string
		userID          string
		sourceFormat    string
		filename        string
		fileHash        string
		targetAccountID string
		wantErr         error
	}{
		{"empty id", "", "user-1", "csv", "f.csv", "hash", "acc-1", importing.ErrImportBatchEmptyID},
		{"empty user id", "batch-1", "", "csv", "f.csv", "hash", "acc-1", importing.ErrImportBatchEmptyUserID},
		{"empty source format", "batch-1", "user-1", "", "f.csv", "hash", "acc-1", importing.ErrImportBatchEmptySourceFormat},
		{"empty filename", "batch-1", "user-1", "csv", "", "hash", "acc-1", importing.ErrImportBatchEmptyFilename},
		{"empty file hash", "batch-1", "user-1", "csv", "f.csv", "", "acc-1", importing.ErrImportBatchEmptyFileHash},
		{"empty target account id", "batch-1", "user-1", "csv", "f.csv", "hash", "", importing.ErrImportBatchEmptyTargetAccountID},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := importing.NewImportBatch(tt.id, tt.userID, tt.sourceFormat, tt.filename, tt.fileHash, tt.targetAccountID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewImportBatch(...) error = %v, want %v", err, tt.wantErr)
			}
		})
	}

	t.Run("WithStatus overrides the default for reconstruction", func(t *testing.T) {
		t.Parallel()
		b, err := importing.NewImportBatch("batch-1", "user-1", "csv", "f.csv", "hash", "acc-1",
			importing.WithStatus(importing.ImportBatchStatusCommitted))
		if err != nil {
			t.Fatalf("NewImportBatch(...) = %v, want success", err)
		}
		if b.Status() != importing.ImportBatchStatusCommitted {
			t.Errorf("Status() = %q, want %q", b.Status(), importing.ImportBatchStatusCommitted)
		}
	})

	t.Run("WithStatus rejects an unknown status", func(t *testing.T) {
		t.Parallel()
		_, err := importing.NewImportBatch("batch-1", "user-1", "csv", "f.csv", "hash", "acc-1",
			importing.WithStatus(importing.ImportBatchStatus("bogus")))
		if !errors.Is(err, importing.ErrImportBatchInvalidStatus) {
			t.Fatalf("NewImportBatch(...) error = %v, want ErrImportBatchInvalidStatus", err)
		}
	})
}

// TestImportBatch_StatusStateMachine exercises every transition
// ADR-0008's staged -> reviewed -> committed -> rolled_back pipeline
// permits, and confirms every other transition attempt is rejected.
func TestImportBatch_StatusStateMachine(t *testing.T) {
	t.Parallel()

	t.Run("the happy path runs staged -> reviewed -> committed -> rolled_back", func(t *testing.T) {
		t.Parallel()
		b := mustBatch(t)

		reviewed, err := b.MarkReviewed()
		if err != nil {
			t.Fatalf("MarkReviewed() = %v, want success", err)
		}
		if reviewed.Status() != importing.ImportBatchStatusReviewed {
			t.Errorf("Status() = %q, want %q", reviewed.Status(), importing.ImportBatchStatusReviewed)
		}
		// b itself must be unchanged (copy-transform, not mutation).
		if b.Status() != importing.ImportBatchStatusStaged {
			t.Errorf("original batch Status() = %q after MarkReviewed, want unchanged %q", b.Status(), importing.ImportBatchStatusStaged)
		}

		committed, err := reviewed.MarkCommitted()
		if err != nil {
			t.Fatalf("MarkCommitted() = %v, want success", err)
		}
		if committed.Status() != importing.ImportBatchStatusCommitted {
			t.Errorf("Status() = %q, want %q", committed.Status(), importing.ImportBatchStatusCommitted)
		}

		rolledBack, err := committed.MarkRolledBack()
		if err != nil {
			t.Fatalf("MarkRolledBack() = %v, want success", err)
		}
		if rolledBack.Status() != importing.ImportBatchStatusRolledBack {
			t.Errorf("Status() = %q, want %q", rolledBack.Status(), importing.ImportBatchStatusRolledBack)
		}
	})

	for _, tt := range []struct {
		name  string
		from  importing.ImportBatchStatus
		apply func(importing.ImportBatch) (importing.ImportBatch, error)
	}{
		{"MarkReviewed from reviewed", importing.ImportBatchStatusReviewed, importing.ImportBatch.MarkReviewed},
		{"MarkReviewed from committed", importing.ImportBatchStatusCommitted, importing.ImportBatch.MarkReviewed},
		{"MarkReviewed from rolled_back", importing.ImportBatchStatusRolledBack, importing.ImportBatch.MarkReviewed},
		{"MarkCommitted from staged", importing.ImportBatchStatusStaged, importing.ImportBatch.MarkCommitted},
		{"MarkCommitted from committed", importing.ImportBatchStatusCommitted, importing.ImportBatch.MarkCommitted},
		{"MarkCommitted from rolled_back", importing.ImportBatchStatusRolledBack, importing.ImportBatch.MarkCommitted},
		{"MarkRolledBack from staged", importing.ImportBatchStatusStaged, importing.ImportBatch.MarkRolledBack},
		{"MarkRolledBack from reviewed", importing.ImportBatchStatusReviewed, importing.ImportBatch.MarkRolledBack},
		{"MarkRolledBack from rolled_back", importing.ImportBatchStatusRolledBack, importing.ImportBatch.MarkRolledBack},
	} {
		tt := tt
		t.Run("rejects "+tt.name, func(t *testing.T) {
			t.Parallel()
			b, err := importing.NewImportBatch("batch-1", "user-1", "csv", "f.csv", "hash", "acc-1", importing.WithStatus(tt.from))
			if err != nil {
				t.Fatalf("NewImportBatch(...) = %v, want success", err)
			}
			if _, err := tt.apply(b); !errors.Is(err, importing.ErrImportBatchInvalidTransition) {
				t.Fatalf("transition error = %v, want ErrImportBatchInvalidTransition", err)
			}
		})
	}
}
