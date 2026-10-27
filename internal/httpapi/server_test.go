package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hanfezh/restock-notification/internal/app"
	"github.com/hanfezh/restock-notification/internal/httpapi"
	"github.com/hanfezh/restock-notification/internal/store"
)

func TestHTTPRestockFlow(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	db := store.OpenTestDB(t)
	svc := app.NewService(db, func() time.Time { return now })
	worker := app.NewWorker(db, func() time.Time { return now })
	server := httptest.NewServer(httpapi.NewServer(svc, worker))
	t.Cleanup(server.Close)

	var product struct {
		ID int64 `json:"id"`
	}
	postJSON(t, server.URL+"/products", map[string]any{
		"sku": "sku-http", "name": "Watch", "stock": 0,
	}, http.StatusCreated, &product)

	productURL := fmt.Sprintf("%s/products/%d", server.URL, product.ID)
	postJSON(t, productURL+"/registrations", map[string]any{
		"email": "a@example.com",
	}, http.StatusCreated, nil)
	postJSON(t, productURL+"/replenish", map[string]any{
		"quantity": 3,
	}, http.StatusOK, nil)
	postJSON(t, server.URL+"/worker/process", map[string]any{
		"limit": 10,
	}, http.StatusOK, nil)

	resp, err := http.Get(server.URL + "/notifications")
	if err != nil {
		t.Fatalf("get notifications: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body []app.NotificationView
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode notifications: %v", err)
	}
	if len(body) != 1 || body[0].JobStatus != "sent" {
		t.Fatalf("expected one sent notification, got %+v", body)
	}
}

func TestHTTPMuteWindowUsesCustomerIDPath(t *testing.T) {
	now := time.Date(2026, 10, 27, 23, 30, 0, 0, time.UTC)
	db := store.OpenTestDB(t)
	svc := app.NewService(db, func() time.Time { return now })
	worker := app.NewWorker(db, func() time.Time { return now })
	server := httptest.NewServer(httpapi.NewServer(svc, worker))
	t.Cleanup(server.Close)

	var product struct {
		ID int64 `json:"id"`
	}
	postJSON(t, server.URL+"/products", map[string]any{
		"sku": "sku-http-mute", "name": "Coat", "stock": 0,
	}, http.StatusCreated, &product)

	var registration app.Registration
	productURL := fmt.Sprintf("%s/products/%d", server.URL, product.ID)
	postJSON(t, productURL+"/registrations", map[string]any{
		"email": "quiet@example.com",
	}, http.StatusCreated, &registration)

	start, end := 23*60, 7*60
	customerURL := fmt.Sprintf("%s/customers/%d/mute-window", server.URL, registration.CustomerID)
	putJSON(t, customerURL, map[string]any{
		"email":        "decoy@example.com",
		"start_minute": start,
		"end_minute":   end,
	}, http.StatusOK, nil)

	postJSON(t, productURL+"/replenish", map[string]any{
		"quantity": 1,
	}, http.StatusOK, nil)

	var result app.WorkerResult
	postJSON(t, server.URL+"/worker/process", map[string]any{
		"limit": 10,
	}, http.StatusOK, &result)
	if result.Deferred != 1 || result.Sent != 0 {
		t.Fatalf("expected one deferred notification, got %+v", result)
	}

	resp, err := http.Get(server.URL + "/notifications")
	if err != nil {
		t.Fatalf("get notifications: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body []app.NotificationView
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode notifications: %v", err)
	}
	wantAvailable := time.Date(2026, 10, 28, 7, 0, 0, 0, time.UTC)
	if len(body) != 1 || body[0].JobStatus != "pending" || body[0].SentAt != nil || !body[0].AvailableAt.Equal(wantAvailable) {
		t.Fatalf("expected one deferred notification available at %s, got %+v", wantAvailable, body)
	}
}

func postJSON(t *testing.T, url string, in any, wantStatus int, out any) {
	t.Helper()
	requestJSON(t, http.MethodPost, url, in, wantStatus, out)
}

func putJSON(t *testing.T, url string, in any, wantStatus int, out any) {
	t.Helper()
	requestJSON(t, http.MethodPut, url, in, wantStatus, out)
}

func requestJSON(t *testing.T, method string, url string, in any, wantStatus int, out any) {
	t.Helper()
	payload, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("build %s %s: %v", method, url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: expected status %d, got %d", method, url, wantStatus, resp.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
}
