package store

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := Migrate(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func OpenTestDB(t testing.TB) *sql.DB {
	t.Helper()
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close test db: %v", err)
		}
	})
	return db
}

func Migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, schema)
	return err
}

const schema = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS products (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	sku TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	stock INTEGER NOT NULL CHECK (stock >= 0),
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS customers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	email TEXT NOT NULL UNIQUE,
	mute_start_minute INTEGER,
	mute_end_minute INTEGER,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	CHECK (
		(mute_start_minute IS NULL AND mute_end_minute IS NULL)
		OR
		(mute_start_minute BETWEEN 0 AND 1439 AND mute_end_minute BETWEEN 0 AND 1439)
	)
);

CREATE TABLE IF NOT EXISTS restock_registrations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	product_id INTEGER NOT NULL REFERENCES products(id),
	customer_id INTEGER NOT NULL REFERENCES customers(id),
	status TEXT NOT NULL CHECK (status IN ('active', 'queued', 'notified')),
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uniq_live_registration
ON restock_registrations(product_id, customer_id)
WHERE status IN ('active', 'queued');

CREATE TABLE IF NOT EXISTS restock_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	product_id INTEGER NOT NULL REFERENCES products(id),
	previous_stock INTEGER NOT NULL,
	new_stock INTEGER NOT NULL,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS notification_jobs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	registration_id INTEGER NOT NULL UNIQUE REFERENCES restock_registrations(id),
	restock_event_id INTEGER NOT NULL REFERENCES restock_events(id),
	customer_id INTEGER NOT NULL REFERENCES customers(id),
	product_id INTEGER NOT NULL REFERENCES products(id),
	status TEXT NOT NULL CHECK (status IN ('pending', 'processing', 'sent', 'failed')),
	available_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	attempt_count INTEGER NOT NULL DEFAULT 0,
	last_error TEXT,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_notification_jobs_due
ON notification_jobs(status, available_at, id);

CREATE TABLE IF NOT EXISTS notification_deliveries (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	job_id INTEGER NOT NULL UNIQUE REFERENCES notification_jobs(id),
	customer_id INTEGER NOT NULL REFERENCES customers(id),
	product_id INTEGER NOT NULL REFERENCES products(id),
	message TEXT NOT NULL,
	sent_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`
