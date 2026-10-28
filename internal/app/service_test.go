package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hanfezh/restock-notification/internal/app"
	"github.com/hanfezh/restock-notification/internal/store"
)

func newService(t *testing.T) *app.Service {
	t.Helper()
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	return app.NewService(store.OpenTestDB(t), func() time.Time { return now })
}

func TestRegisterRestockRequiresOutOfStockProduct(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)

	product, err := svc.CreateProduct(ctx, app.CreateProductInput{
		SKU: "sku-1", Name: "Sneaker", Stock: 3,
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}

	_, err = svc.RegisterRestock(ctx, app.RegisterRestockInput{
		ProductID: product.ID,
		Email:     "a@example.com",
	})
	if err == nil {
		t.Fatal("expected registering for in-stock product to fail")
	}
}

func TestCreateProductRejectsDuplicateSKUAsConflict(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)

	if _, err := svc.CreateProduct(ctx, app.CreateProductInput{
		SKU: "sku-conflict", Name: "Sneaker", Stock: 0,
	}); err != nil {
		t.Fatalf("create product: %v", err)
	}

	_, err := svc.CreateProduct(ctx, app.CreateProductInput{
		SKU: "sku-conflict", Name: "Sneaker 2", Stock: 0,
	})
	if !errors.Is(err, app.ErrConflict) {
		t.Fatalf("expected duplicate SKU conflict, got %v", err)
	}
}

func TestRegisterRestockRejectsDuplicateLiveRegistrationAsConflict(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)

	product, err := svc.CreateProduct(ctx, app.CreateProductInput{
		SKU: "sku-live-registration", Name: "Sneaker", Stock: 0,
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	if _, err := svc.RegisterRestock(ctx, app.RegisterRestockInput{
		ProductID: product.ID,
		Email:     "a@example.com",
	}); err != nil {
		t.Fatalf("register first: %v", err)
	}

	_, err = svc.RegisterRestock(ctx, app.RegisterRestockInput{
		ProductID: product.ID,
		Email:     "a@example.com",
	})
	if !errors.Is(err, app.ErrConflict) {
		t.Fatalf("expected duplicate registration conflict, got %v", err)
	}
}

func TestReplenishCreatesOneJobPerActiveRegistration(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)

	product, err := svc.CreateProduct(ctx, app.CreateProductInput{
		SKU: "sku-2", Name: "Hoodie", Stock: 0,
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	for _, email := range []string{"a@example.com", "b@example.com"} {
		if _, err := svc.RegisterRestock(ctx, app.RegisterRestockInput{
			ProductID: product.ID,
			Email:     email,
		}); err != nil {
			t.Fatalf("register %s: %v", email, err)
		}
	}

	result, err := svc.Replenish(ctx, product.ID, 5)
	if err != nil {
		t.Fatalf("replenish: %v", err)
	}
	if result.RestockEventID == 0 {
		t.Fatal("expected restock event id")
	}
	if result.CreatedJobs != 2 {
		t.Fatalf("expected 2 jobs, got %d", result.CreatedJobs)
	}

	views, err := svc.ListNotifications(ctx)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("expected 2 notification jobs, got %d", len(views))
	}
	for _, view := range views {
		if view.JobStatus != "pending" {
			t.Fatalf("expected pending job, got %+v", view)
		}
	}
}

func TestReplenishDoesNotReuseCompletedOneShotRegistration(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	db := store.OpenTestDB(t)
	svc := app.NewService(db, func() time.Time { return now })
	worker := app.NewWorker(db, func() time.Time { return now })

	product, err := svc.CreateProduct(ctx, app.CreateProductInput{
		SKU: "sku-completed-registration", Name: "Hoodie", Stock: 0,
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	if _, err := svc.RegisterRestock(ctx, app.RegisterRestockInput{
		ProductID: product.ID,
		Email:     "a@example.com",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.Replenish(ctx, product.ID, 5); err != nil {
		t.Fatalf("first replenish: %v", err)
	}
	if result, err := worker.ProcessBatch(ctx, 10); err != nil {
		t.Fatalf("process batch: %v", err)
	} else if result.Sent != 1 {
		t.Fatalf("expected first job to send, got %+v", result)
	}

	if _, err := db.ExecContext(ctx, `UPDATE products SET stock = 0 WHERE id = ?`, product.ID); err != nil {
		t.Fatalf("simulate later stockout: %v", err)
	}
	result, err := svc.Replenish(ctx, product.ID, 3)
	if err != nil {
		t.Fatalf("second replenish: %v", err)
	}
	if result.RestockEventID == 0 || result.CreatedJobs != 0 {
		t.Fatalf("expected later restock event with no jobs for completed registration, got %+v", result)
	}

	views, err := svc.ListNotifications(ctx)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(views) != 1 || views[0].JobStatus != "sent" {
		t.Fatalf("expected only original sent notification, got %+v", views)
	}
}
