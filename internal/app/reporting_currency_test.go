package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func TestGetReportingCurrency_UnsetReturnsEmptyString(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")

	got, err := svc.GetReportingCurrency(context.Background(), testActorID)
	if err != nil {
		t.Fatalf("GetReportingCurrency: %v", err)
	}
	if got != "" {
		t.Errorf("GetReportingCurrency = %q, want \"\" (unset)", got)
	}
}

func TestSetReportingCurrency_ThenGetRoundTrips(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	if err := svc.SetReportingCurrency(ctx, testActorID, "EUR"); err != nil {
		t.Fatalf("SetReportingCurrency: %v", err)
	}

	got, err := svc.GetReportingCurrency(ctx, testActorID)
	if err != nil {
		t.Fatalf("GetReportingCurrency: %v", err)
	}
	if got != "EUR" {
		t.Errorf("GetReportingCurrency = %q, want %q", got, "EUR")
	}
}

func TestSetReportingCurrency_UnknownCodeRejected(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")

	err := svc.SetReportingCurrency(context.Background(), testActorID, "NOTREAL")
	wantErrCode(t, err, errs.InvalidInput)
}

func TestReportingCurrencyUseCases_RequireActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	_, err := svc.GetReportingCurrency(ctx, "")
	wantErrCode(t, err, errs.InvalidInput)

	err = svc.SetReportingCurrency(ctx, "", "EUR")
	wantErrCode(t, err, errs.InvalidInput)
}

func TestGetReportingCurrency_UnknownActorIsNotFound(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")

	_, err := svc.GetReportingCurrency(context.Background(), "does-not-exist")
	wantErrCode(t, err, errs.NotFound)
}
