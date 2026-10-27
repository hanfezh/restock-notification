package app_test

import (
	"context"
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
