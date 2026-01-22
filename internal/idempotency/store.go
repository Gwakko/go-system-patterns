package idempotency

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var (
	ErrKeyAlreadyUsed = errors.New("idempotency key already used")
	ErrKeyNotFound    = errors.New("idempotency key not found")
	ErrKeyExpired     = errors.New("idempotency key expired")
)

type Key struct {
	Key       string    `json:"key"`
	Status    string    `json:"status"` // "created", "processing", "completed", "failed"
	Response  []byte    `json:"response,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Store struct {
	db  *sql.DB
	ttl time.Duration
}

func NewStore(db *sql.DB, ttl time.Duration) *Store {
	return &Store{db: db, ttl: ttl}
}

// Generate creates a new idempotency key with expiration.
func (s *Store) Generate(ctx context.Context) (*Key, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("idempotency.Generate: rand: %w", err)
	}

	key := &Key{
		Key:       "idk_" + hex.EncodeToString(bytes),
		Status:    "created",
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(s.ttl),
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO idempotency_keys (key, status, created_at, expires_at)
		 VALUES ($1, $2, $3, $4)`,
		key.Key, key.Status, key.CreatedAt, key.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("idempotency.Generate: insert: %w", err)
	}

	return key, nil
}

// Acquire attempts to lock the key for processing.
// Returns the cached response if the key was already completed.
// Uses SELECT ... FOR UPDATE to prevent concurrent use.
func (s *Store) Acquire(ctx context.Context, tx *sql.Tx, keyStr string) (*Key, error) {
	var key Key
	err := tx.QueryRowContext(ctx,
		`SELECT key, status, response, created_at, expires_at
		 FROM idempotency_keys
		 WHERE key = $1
		 FOR UPDATE`,
		keyStr,
	).Scan(&key.Key, &key.Status, &key.Response, &key.CreatedAt, &key.ExpiresAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("idempotency.Acquire: select: %w", err)
	}

	if time.Now().UTC().After(key.ExpiresAt) {
		return nil, ErrKeyExpired
	}

	switch key.Status {
	case "completed":
		return &key, ErrKeyAlreadyUsed
	case "processing":
		return nil, ErrKeyAlreadyUsed
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE idempotency_keys SET status = 'processing' WHERE key = $1`,
		keyStr,
	)
	if err != nil {
		return nil, fmt.Errorf("idempotency.Acquire: update status: %w", err)
	}

	return &key, nil
}

// Complete marks the key as completed and stores the cached response.
func (s *Store) Complete(ctx context.Context, tx *sql.Tx, keyStr string, response []byte) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE idempotency_keys SET status = 'completed', response = $1 WHERE key = $2`,
		response, keyStr,
	)
	if err != nil {
		return fmt.Errorf("idempotency.Complete: %w", err)
	}
	return nil
}

// Fail marks the key as failed so it can be retried.
func (s *Store) Fail(ctx context.Context, tx *sql.Tx, keyStr string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE idempotency_keys SET status = 'created' WHERE key = $1`,
		keyStr,
	)
	if err != nil {
		return fmt.Errorf("idempotency.Fail: %w", err)
	}
	return nil
}
