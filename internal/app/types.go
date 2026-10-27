package app

import "time"

type Product struct {
	ID    int64  `json:"id"`
	SKU   string `json:"sku"`
	Name  string `json:"name"`
	Stock int    `json:"stock"`
}

type Registration struct {
	ID         int64  `json:"id"`
	ProductID  int64  `json:"product_id"`
	CustomerID int64  `json:"customer_id"`
	Status     string `json:"status"`
}

type CreateProductInput struct {
	SKU   string `json:"sku"`
	Name  string `json:"name"`
	Stock int    `json:"stock"`
}

type RegisterRestockInput struct {
	ProductID int64  `json:"product_id"`
	Email     string `json:"email"`
}

type ReplenishResult struct {
	ProductID      int64 `json:"product_id"`
	PreviousStock  int   `json:"previous_stock"`
	NewStock       int   `json:"new_stock"`
	RestockEventID int64 `json:"restock_event_id,omitempty"`
	CreatedJobs    int   `json:"created_jobs"`
}

type WorkerResult struct {
	Claimed  int `json:"claimed"`
	Sent     int `json:"sent"`
	Deferred int `json:"deferred"`
	Failed   int `json:"failed"`
}

type SetMuteWindowInput struct {
	Email       string `json:"email"`
	StartMinute *int   `json:"start_minute"`
	EndMinute   *int   `json:"end_minute"`
}

type NotificationView struct {
	JobID         int64      `json:"job_id"`
	JobStatus     string     `json:"job_status"`
	CustomerEmail string     `json:"customer_email"`
	ProductID     int64      `json:"product_id"`
	AvailableAt   time.Time  `json:"available_at"`
	SentAt        *time.Time `json:"sent_at,omitempty"`
	Message       string     `json:"message,omitempty"`
}
