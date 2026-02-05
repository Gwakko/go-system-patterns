package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"

	"github.com/gwakko/go-system-patterns/internal/circuitbreaker"
	"github.com/gwakko/go-system-patterns/internal/idempotency"
	mw "github.com/gwakko/go-system-patterns/internal/middleware"
	"github.com/gwakko/go-system-patterns/internal/ratelimit"
	"github.com/gwakko/go-system-patterns/internal/transfer"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/patterns?sslmode=disable"
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatalf("db ping: %v", err)
	}

	// Initialize components
	idemStore := idempotency.NewStore(db, 24*time.Hour)
	breaker := circuitbreaker.New(5, 2, 30*time.Second)
	limiter := ratelimit.NewPerClient(100, 10) // 100 max, 10/sec refill

	transferService := transfer.NewService(db, idemStore, breaker)

	idemHandler := idempotency.NewHandler(idemStore)
	transferHandler := transfer.NewHandler(transferService)

	// Routes
	mux := http.NewServeMux()

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","circuit_breaker":"%s"}`, breaker.State())
	})

	mux.HandleFunc("/api/idempotency-keys", idemHandler.HandleGenerate)
	mux.HandleFunc("/api/transfers", transferHandler.HandleTransfer)

	// Middleware chain: rate limit → idempotency check → handler
	handler := mw.RateLimit(limiter)(mw.RequireIdempotencyKey(mux))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on :%s", port)
	log.Printf("  POST /api/idempotency-keys  — generate key")
	log.Printf("  POST /api/transfers          — execute transfer (requires Idempotency-Key header)")
	log.Printf("  GET  /api/health             — health + circuit breaker state")

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}
