package store_test

import (
	"context"
	"testing"

	"github.com/hanfezh/restock-notification/internal/store"
)

func TestMigrateCreatesRequiredTables(t *testing.T) {
	db := store.OpenTestDB(t)

	rows, err := db.QueryContext(context.Background(), `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table'
		ORDER BY name`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()

	tables := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		tables[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table names: %v", err)
	}

	for _, name := range []string{
		"products",
		"customers",
		"restock_registrations",
		"restock_events",
		"notification_jobs",
		"notification_deliveries",
	} {
		if !tables[name] {
			t.Fatalf("expected table %s to exist; got %#v", name, tables)
		}
	}
}
