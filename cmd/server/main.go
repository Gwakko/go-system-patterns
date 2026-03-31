package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/gwakko/go-system-patterns/internal/account"
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
	accountHandler := account.NewHandler(db)

	// Routes
	mux := http.NewServeMux()

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","circuit_breaker":"%s"}`, breaker.State())
	})

	mux.HandleFunc("/api/idempotency-keys", idemHandler.HandleGenerate)
	mux.HandleFunc("/api/transfers", transferHandler.HandleTransfer)

	// Account routes
	mux.HandleFunc("/api/accounts/", func(w http.ResponseWriter, r *http.Request) {
		// Route to transactions handler if path ends with /transactions
		if len(r.URL.Path) > len("/api/accounts/") {
			path := r.URL.Path[len("/api/accounts/"):]
			if idx := len(path) - len("/transactions"); idx > 0 && path[idx:] == "/transactions" {
				accountHandler.HandleGetTransactions(w, r)
				return
			}
		}
		accountHandler.HandleGetAccount(w, r)
	})

	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	// Middleware chain: metrics → rate limit → idempotency check → handler
	handler := mw.Metrics(mw.RateLimit(limiter)(mw.RequireIdempotencyKey(mux)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: handler,
	}

	// Graceful shutdown: listen for SIGINT/SIGTERM
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("Server starting on :%s", port)
		log.Printf("  POST /api/idempotency-keys          — generate key")
		log.Printf("  POST /api/transfers                  — execute transfer (requires Idempotency-Key header)")
		log.Printf("  GET  /api/health                     — health + circuit breaker state")
		log.Printf("  GET  /api/accounts/{id}              — get account with balance")
		log.Printf("  GET  /api/accounts/{id}/transactions — list transfers for account")
		log.Printf("  GET  /metrics                        — Prometheus metrics")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-shutdown
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server shutdown: %v", err)
	}

	log.Println("server stopped")
}
