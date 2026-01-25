package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Event struct {
	ID          string          `json:"id"`
	AggregateID string          `json:"aggregate_id"`
	EventType   string          `json:"event_type"`
	Payload     json.RawMessage `json:"payload"`
	CreatedAt   time.Time       `json:"created_at"`
}

// Write inserts an event into the outbox table within an existing transaction.
// This guarantees atomicity: the business write and the event are committed together.
func Write(ctx context.Context, tx *sql.Tx, aggregateID, eventType string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("outbox.Write: marshal payload: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO outbox (id, aggregate_id, event_type, payload, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		uuid.NewString(), aggregateID, eventType, data, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("outbox.Write: insert: %w", err)
	}
	return nil
}
