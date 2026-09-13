package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func TestMCPToolCallRepository_CreateAndList(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewMCPToolCallRepository(db)
	ctx := context.Background()

	calledAt := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	token := "tok-123"
	call := ports.MCPToolCall{
		ID:                "call-1",
		UserID:            ports.SeededUserID,
		ToolName:          "delete_transaction",
		Tier:              ports.MCPToolTierDestructive,
		Arguments:         `{"transaction_ref":"txn-1"}`,
		ConfirmationToken: &token,
		Result:            "ok",
		CalledAt:          calledAt,
	}
	if err := repo.Create(ctx, ports.SeededUserID, call); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.List(ctx, ports.SeededUserID, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List returned %d rows, want 1", len(got))
	}
	row := got[0]
	if row.ID != "call-1" || row.ToolName != "delete_transaction" || row.Tier != ports.MCPToolTierDestructive {
		t.Errorf("List()[0] = %+v", row)
	}
	if row.Arguments != call.Arguments {
		t.Errorf("Arguments = %q, want %q", row.Arguments, call.Arguments)
	}
	if row.ConfirmationToken == nil || *row.ConfirmationToken != token {
		t.Errorf("ConfirmationToken = %v, want %q", row.ConfirmationToken, token)
	}
	if row.Result != "ok" {
		t.Errorf("Result = %q, want %q", row.Result, "ok")
	}
	if !row.CalledAt.Equal(calledAt) {
		t.Errorf("CalledAt = %v, want %v", row.CalledAt, calledAt)
	}
}

func TestMCPToolCallRepository_Create_NullConfirmationToken(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewMCPToolCallRepository(db)
	ctx := context.Background()

	call := ports.MCPToolCall{
		ID:        "call-2",
		UserID:    ports.SeededUserID,
		ToolName:  "record_outflow",
		Tier:      ports.MCPToolTierWrite,
		Arguments: `{"amount":"12.00"}`,
		Result:    "ok",
		CalledAt:  time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC),
	}
	if err := repo.Create(ctx, ports.SeededUserID, call); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.List(ctx, ports.SeededUserID, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List returned %d rows, want 1", len(got))
	}
	if got[0].ConfirmationToken != nil {
		t.Errorf("ConfirmationToken = %v, want nil for a write-tier call", *got[0].ConfirmationToken)
	}
}

func TestMCPToolCallRepository_List_OrdersMostRecentFirstAndRespectsLimit(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewMCPToolCallRepository(db)
	ctx := context.Background()

	base := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	for i, name := range []string{"whoami", "record_outflow", "record_inflow"} {
		call := ports.MCPToolCall{
			ID:        "call-" + name,
			UserID:    ports.SeededUserID,
			ToolName:  name,
			Tier:      ports.MCPToolTierWrite,
			Arguments: "{}",
			Result:    "ok",
			CalledAt:  base.Add(time.Duration(i) * time.Minute),
		}
		if err := repo.Create(ctx, ports.SeededUserID, call); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
	}

	got, err := repo.List(ctx, ports.SeededUserID, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d rows, want 2 (limit)", len(got))
	}
	if got[0].ToolName != "record_inflow" || got[1].ToolName != "record_outflow" {
		t.Errorf("List order = [%s, %s], want most-recent-first [record_inflow, record_outflow]", got[0].ToolName, got[1].ToolName)
	}
}

func TestMCPToolCallRepository_List_ScopedToActor(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewMCPToolCallRepository(db)
	ctx := context.Background()

	call := ports.MCPToolCall{
		ID:        "call-other",
		UserID:    ports.SeededUserID,
		ToolName:  "whoami",
		Tier:      ports.MCPToolTierWrite,
		Arguments: "{}",
		Result:    "ok",
		CalledAt:  time.Now().UTC(),
	}
	if err := repo.Create(ctx, ports.SeededUserID, call); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.List(ctx, "some-other-user", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List(other actor) returned %d rows, want 0", len(got))
	}
}

func TestMCPToolCallRepository_Create_RejectsMismatchedActor(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewMCPToolCallRepository(db)
	ctx := context.Background()

	call := ports.MCPToolCall{
		ID:        "call-mismatch",
		UserID:    ports.SeededUserID,
		ToolName:  "whoami",
		Tier:      ports.MCPToolTierRead,
		Arguments: "{}",
		Result:    "ok",
		CalledAt:  time.Now().UTC(),
	}
	err := repo.Create(ctx, "someone-else", call)
	if err == nil {
		t.Fatal("Create(actorID != call.UserID) returned no error, want NotAllowed")
	}
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Errorf("Create(actorID != call.UserID) error = %v, want *errs.Error{Code: NotAllowed}", err)
	}
}
