package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/gwakko/go-system-patterns/internal/outbox"
)

// StdoutPublisher logs events to stdout (replace with Kafka/RabbitMQ client).
type StdoutPublisher struct{}

func (p *StdoutPublisher) Publish(_ context.Context, eventType string, payload json.RawMessage) error {
	log.Printf("PUBLISH [%s]: %s", eventType, string(payload))
	return nil
}

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

	publisher := &StdoutPublisher{}
	relay := outbox.NewRelay(db, publisher, 50, 2*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down relay...")
		cancel()
	}()

	if err := relay.Start(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("relay: %v", err)
	}
}
