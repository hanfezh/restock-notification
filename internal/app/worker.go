package app

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Worker struct {
	db    *sql.DB
	clock func() time.Time
}

func NewWorker(db *sql.DB, clock func() time.Time) *Worker {
	if clock == nil {
		clock = time.Now
	}
	return &Worker{db: db, clock: clock}
}

func (w *Worker) ProcessBatch(ctx context.Context, limit int) (WorkerResult, error) {
	if limit <= 0 {
		limit = 10
	}
	now := w.clock().UTC()
	rows, err := w.db.QueryContext(ctx, `
		SELECT j.id, j.registration_id, j.customer_id, j.product_id, c.email, c.mute_start_minute, c.mute_end_minute
		FROM notification_jobs j
		JOIN customers c ON c.id = j.customer_id
		WHERE j.status = 'pending' AND j.available_at <= ?
		ORDER BY j.id
		LIMIT ?`, formatDBTime(now), limit)
	if err != nil {
		return WorkerResult{}, err
	}
	defer rows.Close()

	type job struct {
		id, registrationID, customerID, productID int64
		email                                     string
		start, end                                sql.NullInt64
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.registrationID, &j.customerID, &j.productID, &j.email, &j.start, &j.end); err != nil {
			return WorkerResult{}, err
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return WorkerResult{}, err
	}

	result := WorkerResult{Claimed: len(jobs)}
	for _, j := range jobs {
		if err := w.processJob(ctx, j.id, j.registrationID, j.customerID, j.productID, j.email, nullMinute(j.start), nullMinute(j.end), now, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (w *Worker) processJob(ctx context.Context, jobID int64, registrationID int64, customerID int64, productID int64, email string, start *int, end *int, now time.Time, result *WorkerResult) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		UPDATE notification_jobs
		SET status = 'processing', updated_at = ?
		WHERE id = ? AND status = 'pending'`, formatDBTime(now), jobID)
	if err != nil {
		return err
	}
	claimed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if claimed == 0 {
		return tx.Commit()
	}

	if IsMutedAt(start, end, now) {
		availableAt := MuteEndAfter(*start, *end, now).UTC()
		if _, err := tx.ExecContext(ctx, `
			UPDATE notification_jobs
			SET status = 'pending', available_at = ?, updated_at = ?
			WHERE id = ?`, formatDBTime(availableAt), formatDBTime(now), jobID); err != nil {
			return err
		}
		result.Deferred++
		return tx.Commit()
	}

	message := fmt.Sprintf("Product %d is back in stock for %s", productID, email)
	res, err = tx.ExecContext(ctx, `
		INSERT INTO notification_deliveries (job_id, customer_id, product_id, message, sent_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(job_id) DO NOTHING`, jobID, customerID, productID, message, formatDBTime(now))
	if err != nil {
		return err
	}
	delivered, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE notification_jobs
		SET status = 'sent', updated_at = ?
		WHERE id = ?`, formatDBTime(now), jobID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE restock_registrations
		SET status = 'notified', updated_at = ?
		WHERE id = ?`, formatDBTime(now), registrationID); err != nil {
		return err
	}
	if delivered > 0 {
		result.Sent++
	}
	return tx.Commit()
}

func IsMutedAt(start *int, end *int, now time.Time) bool {
	if start == nil || end == nil {
		return false
	}
	minute := now.Hour()*60 + now.Minute()
	if *start == *end {
		return false
	}
	if *start < *end {
		return minute >= *start && minute < *end
	}
	return minute >= *start || minute < *end
}

func MuteEndAfter(start int, end int, now time.Time) time.Time {
	base := time.Date(now.Year(), now.Month(), now.Day(), end/60, end%60, 0, 0, now.Location())
	if start > end && (now.Hour()*60+now.Minute()) >= start {
		return base.Add(24 * time.Hour)
	}
	if !base.After(now) {
		return base.Add(24 * time.Hour)
	}
	return base
}

func nullMinute(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	minute := int(value.Int64)
	return &minute
}

func formatDBTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339)
}
