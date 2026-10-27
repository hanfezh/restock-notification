package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrProductInStock = errors.New("product is in stock")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
)

type Service struct {
	db    *sql.DB
	clock func() time.Time
}

func NewService(db *sql.DB, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{db: db, clock: clock}
}

func (s *Service) CreateProduct(ctx context.Context, in CreateProductInput) (Product, error) {
	if strings.TrimSpace(in.SKU) == "" || strings.TrimSpace(in.Name) == "" || in.Stock < 0 {
		return Product{}, ErrInvalidInput
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO products (sku, name, stock, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)`, in.SKU, in.Name, in.Stock)
	if err != nil {
		return Product{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Product{}, err
	}
	return Product{ID: id, SKU: in.SKU, Name: in.Name, Stock: in.Stock}, nil
}

func (s *Service) RegisterRestock(ctx context.Context, in RegisterRestockInput) (Registration, error) {
	if in.ProductID == 0 || strings.TrimSpace(in.Email) == "" {
		return Registration{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Registration{}, err
	}
	defer tx.Rollback()

	var stock int
	if err := tx.QueryRowContext(ctx, `SELECT stock FROM products WHERE id = ?`, in.ProductID).Scan(&stock); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Registration{}, ErrNotFound
		}
		return Registration{}, err
	}
	if stock > 0 {
		return Registration{}, ErrProductInStock
	}

	customerID, err := upsertCustomer(ctx, tx, in.Email)
	if err != nil {
		return Registration{}, err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO restock_registrations (product_id, customer_id, status, updated_at)
		VALUES (?, ?, 'active', CURRENT_TIMESTAMP)`, in.ProductID, customerID)
	if err != nil {
		return Registration{}, err
	}
	registrationID, err := res.LastInsertId()
	if err != nil {
		return Registration{}, err
	}
	if err := tx.Commit(); err != nil {
		return Registration{}, err
	}
	return Registration{ID: registrationID, ProductID: in.ProductID, CustomerID: customerID, Status: "active"}, nil
}

func (s *Service) Replenish(ctx context.Context, productID int64, quantity int) (ReplenishResult, error) {
	if productID == 0 || quantity <= 0 {
		return ReplenishResult{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReplenishResult{}, err
	}
	defer tx.Rollback()

	var previous int
	if err := tx.QueryRowContext(ctx, `SELECT stock FROM products WHERE id = ?`, productID).Scan(&previous); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReplenishResult{}, ErrNotFound
		}
		return ReplenishResult{}, err
	}
	newStock := previous + quantity
	if _, err := tx.ExecContext(ctx, `
		UPDATE products SET stock = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, newStock, productID); err != nil {
		return ReplenishResult{}, err
	}

	result := ReplenishResult{ProductID: productID, PreviousStock: previous, NewStock: newStock}
	if previous == 0 && newStock > 0 {
		eventID, created, err := createRestockEventAndJobs(ctx, tx, productID, previous, newStock, s.clock().UTC())
		if err != nil {
			return ReplenishResult{}, err
		}
		result.RestockEventID = eventID
		result.CreatedJobs = created
	}
	if err := tx.Commit(); err != nil {
		return ReplenishResult{}, err
	}
	return result, nil
}

func upsertCustomer(ctx context.Context, tx *sql.Tx, email string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO customers (email, updated_at)
		VALUES (?, CURRENT_TIMESTAMP)
		ON CONFLICT(email) DO UPDATE SET email = excluded.email
		RETURNING id`, strings.TrimSpace(email)).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func createRestockEventAndJobs(ctx context.Context, tx *sql.Tx, productID int64, previous int, next int, now time.Time) (int64, int, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO restock_events (product_id, previous_stock, new_stock)
		VALUES (?, ?, ?)`, productID, previous, next)
	if err != nil {
		return 0, 0, err
	}
	eventID, err := res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, customer_id
		FROM restock_registrations
		WHERE product_id = ? AND status = 'active'
		ORDER BY id`, productID)
	if err != nil {
		return 0, 0, err
	}
	type registration struct {
		id         int64
		customerID int64
	}
	var registrations []registration
	for rows.Next() {
		var r registration
		if err := rows.Scan(&r.id, &r.customerID); err != nil {
			_ = rows.Close()
			return 0, 0, err
		}
		registrations = append(registrations, r)
	}
	if err := rows.Close(); err != nil {
		return 0, 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	for _, r := range registrations {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO notification_jobs (registration_id, restock_event_id, customer_id, product_id, status, available_at, updated_at)
			VALUES (?, ?, ?, ?, 'pending', ?, ?)`, r.id, eventID, r.customerID, productID, formatDBTime(now), formatDBTime(now)); err != nil {
			return 0, 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE restock_registrations
			SET status = 'queued', updated_at = CURRENT_TIMESTAMP
			WHERE id = ?`, r.id); err != nil {
			return 0, 0, err
		}
	}
	return eventID, len(registrations), nil
}

func (s *Service) SetMuteWindow(ctx context.Context, in SetMuteWindowInput) error {
	if strings.TrimSpace(in.Email) == "" {
		return ErrInvalidInput
	}
	if (in.StartMinute == nil) != (in.EndMinute == nil) {
		return ErrInvalidInput
	}
	if in.StartMinute != nil {
		if *in.StartMinute < 0 || *in.StartMinute > 1439 || *in.EndMinute < 0 || *in.EndMinute > 1439 {
			return ErrInvalidInput
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	customerID, err := upsertCustomer(ctx, tx, in.Email)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE customers
		SET mute_start_minute = ?, mute_end_minute = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, in.StartMinute, in.EndMinute, customerID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ListNotifications(ctx context.Context) ([]NotificationView, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT j.id, j.status, c.email, j.product_id, j.available_at, d.sent_at, d.message
		FROM notification_jobs j
		JOIN customers c ON c.id = j.customer_id
		LEFT JOIN notification_deliveries d ON d.job_id = j.id
		ORDER BY j.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []NotificationView
	for rows.Next() {
		var view NotificationView
		var availableAt string
		var sentAt sql.NullString
		var message sql.NullString
		if err := rows.Scan(
			&view.JobID,
			&view.JobStatus,
			&view.CustomerEmail,
			&view.ProductID,
			&availableAt,
			&sentAt,
			&message,
		); err != nil {
			return nil, err
		}
		parsedAvailableAt, err := parseDBTime(availableAt)
		if err != nil {
			return nil, fmt.Errorf("parse available_at: %w", err)
		}
		view.AvailableAt = parsedAvailableAt
		if sentAt.Valid {
			parsedSentAt, err := parseDBTime(sentAt.String)
			if err != nil {
				return nil, fmt.Errorf("parse sent_at: %w", err)
			}
			view.SentAt = &parsedSentAt
		}
		if message.Valid {
			view.Message = message.String
		}
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return views, nil
}

func parseDBTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format %q", value)
}
