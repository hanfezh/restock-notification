package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/hanfezh/restock-notification/internal/app"
	"github.com/hanfezh/restock-notification/internal/httpapi"
	"github.com/hanfezh/restock-notification/internal/store"
)

func main() {
	dbPath := getenv("RESTOCK_DB_PATH", "restock.db")
	addr := getenv("RESTOCK_ADDR", ":8080")

	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	clock := time.Now
	service := app.NewService(db, clock)
	worker := app.NewWorker(db, clock)
	handler := httpapi.NewServer(service, worker)

	server := &http.Server{Addr: addr, Handler: handler}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if _, err := worker.ProcessBatch(context.Background(), 25); err != nil {
				log.Printf("process notifications: %v", err)
			}
		}
	}()

	go func() {
		log.Printf("listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func getenv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
