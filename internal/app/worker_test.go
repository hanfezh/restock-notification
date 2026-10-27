package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/hanfezh/restock-notification/internal/app"
	"github.com/hanfezh/restock-notification/internal/store"
)

func TestWorkerSendsEachJobExactlyOnce(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	db := store.OpenTestDB(t)
	svc := app.NewService(db, func() time.Time { return now })
	worker := app.NewWorker(db, func() time.Time { return now })

	product, err := svc.CreateProduct(ctx, app.CreateProductInput{SKU: "sku-3", Name: "Cap", Stock: 0})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	if _, err := svc.RegisterRestock(ctx, app.RegisterRestockInput{ProductID: product.ID, Email: "a@example.com"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.Replenish(ctx, product.ID, 2); err != nil {
		t.Fatalf("replenish: %v", err)
	}

	first, err := worker.ProcessBatch(ctx, 10)
	if err != nil {
		t.Fatalf("process first batch: %v", err)
	}
	second, err := worker.ProcessBatch(ctx, 10)
	if err != nil {
		t.Fatalf("process second batch: %v", err)
	}
	if first.Sent != 1 || second.Sent != 0 {
		t.Fatalf("expected exactly one send, got first=%+v second=%+v", first, second)
	}

	views, err := svc.ListNotifications(ctx)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(views) != 1 || views[0].JobStatus != "sent" || views[0].SentAt == nil {
		t.Fatalf("expected one sent notification, got %+v", views)
	}
}

func TestWorkerDefersMutedCustomerUntilMuteWindowEnds(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 27, 23, 30, 0, 0, time.UTC)
	db := store.OpenTestDB(t)
	svc := app.NewService(db, func() time.Time { return now })
	worker := app.NewWorker(db, func() time.Time { return now })

	product, err := svc.CreateProduct(ctx, app.CreateProductInput{SKU: "sku-4", Name: "Bag", Stock: 0})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	start, end := 23*60, 7*60
	if err := svc.SetMuteWindow(ctx, app.SetMuteWindowInput{
		Email: "quiet@example.com", StartMinute: &start, EndMinute: &end,
	}); err != nil {
		t.Fatalf("set mute window: %v", err)
	}
	if _, err := svc.RegisterRestock(ctx, app.RegisterRestockInput{ProductID: product.ID, Email: "quiet@example.com"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.Replenish(ctx, product.ID, 1); err != nil {
		t.Fatalf("replenish: %v", err)
	}

	result, err := worker.ProcessBatch(ctx, 10)
	if err != nil {
		t.Fatalf("process muted batch: %v", err)
	}
	if result.Deferred != 1 || result.Sent != 0 {
		t.Fatalf("expected one deferred job, got %+v", result)
	}

	views, err := svc.ListNotifications(ctx)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	wantAvailable := time.Date(2026, 10, 28, 7, 0, 0, 0, time.UTC)
	if !views[0].AvailableAt.Equal(wantAvailable) {
		t.Fatalf("expected available_at %s, got %s", wantAvailable, views[0].AvailableAt)
	}
}

func TestWorkerDoesNotClaimFutureSQLiteTimestampOnSameDate(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	db := store.OpenTestDB(t)
	svc := app.NewService(db, func() time.Time { return now })
	worker := app.NewWorker(db, func() time.Time { return now })

	product, err := svc.CreateProduct(ctx, app.CreateProductInput{SKU: "sku-5", Name: "Socks", Stock: 0})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	if _, err := svc.RegisterRestock(ctx, app.RegisterRestockInput{ProductID: product.ID, Email: "future@example.com"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.Replenish(ctx, product.ID, 1); err != nil {
		t.Fatalf("replenish: %v", err)
	}

	futureAvailable := time.Date(2026, 10, 27, 13, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `
		UPDATE notification_jobs
		SET available_at = ?
		WHERE product_id = ?`, futureAvailable.Format("2006-01-02 15:04:05"), product.ID); err != nil {
		t.Fatalf("set future available_at: %v", err)
	}

	result, err := worker.ProcessBatch(ctx, 10)
	if err != nil {
		t.Fatalf("process batch: %v", err)
	}
	if result.Claimed != 0 || result.Sent != 0 || result.Deferred != 0 {
		t.Fatalf("expected future job not to be claimed, got %+v", result)
	}

	views, err := svc.ListNotifications(ctx)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(views) != 1 || views[0].JobStatus != "pending" || views[0].SentAt != nil {
		t.Fatalf("expected future job to remain pending, got %+v", views)
	}
}
