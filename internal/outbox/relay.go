package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Publisher defines how events are published to an external broker.
type Publisher interface {
	Publish(ctx context.Context, eventType string, payload json.RawMessage) error
}

// Relay polls the outbox table and publishes unpublished events.
// Uses SELECT ... FOR UPDATE SKIP LOCKED for concurrent relay instances.
type Relay struct {
	db        *sql.DB
	publisher Publisher
	batchSize int
	interval  time.Duration
}

func NewRelay(db *sql.DB, publisher Publisher, batchSize int, interval time.Duration) *Relay {
	return &Relay{
		db:        db,
		publisher: publisher,
		batchSize: batchSize,
		interval:  interval,
	}
}

// Start runs the relay loop until the context is cancelled.
func (r *Relay) Start(ctx context.Context) error {
	log.Printf("Outbox relay started (batch=%d, interval=%s)", r.batchSize, r.interval)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.processBatch(ctx); err != nil {
				log.Printf("relay batch error: %v", err)
			}
		}
	}
}

func (r *Relay) processBatch(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("relay.processBatch: begin tx: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT id, aggregate_id, event_type, payload, created_at
		 FROM outbox
		 WHERE published_at IS NULL
		 ORDER BY created_at ASC
		 LIMIT $1
		 FOR UPDATE SKIP LOCKED`,
		r.batchSize,
	)
	if err != nil {
		return fmt.Errorf("relay.processBatch: query outbox: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.AggregateID, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			return fmt.Errorf("relay.processBatch: scan row: %w", err)
		}
		events = append(events, e)
	}

	if len(events) == 0 {
		return nil
	}

	for _, e := range events {
		if err := r.publisher.Publish(ctx, e.EventType, e.Payload); err != nil {
			log.Printf("publish failed for event %s: %v", e.ID, err)
			continue
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE outbox SET published_at = $1 WHERE id = $2`,
			time.Now().UTC(), e.ID,
		); err != nil {
			return fmt.Errorf("relay.processBatch: mark published %s: %w", e.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("relay.processBatch: commit: %w", err)
	}
	return nil
}
