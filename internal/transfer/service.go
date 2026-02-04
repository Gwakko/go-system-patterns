package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gwakko/go-system-patterns/internal/circuitbreaker"
	"github.com/gwakko/go-system-patterns/internal/idempotency"
	"github.com/gwakko/go-system-patterns/internal/outbox"
)

var (
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrSameAccount         = errors.New("cannot transfer to the same account")
)

type Transfer struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Amount    int64     `json:"amount"` // cents
	Status    string    `json:"status"` // "completed", "failed"
	CreatedAt time.Time `json:"created_at"`
}

type CreateTransferRequest struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Amount int64  `json:"amount"`
}

type Service struct {
	db          *sql.DB
	idempotency *idempotency.Store
	breaker     *circuitbreaker.Breaker
}

func NewService(db *sql.DB, idemStore *idempotency.Store, breaker *circuitbreaker.Breaker) *Service {
	return &Service{
		db:          db,
		idempotency: idemStore,
		breaker:     breaker,
	}
}

// Execute performs a transfer with idempotency guarantee and outbox event.
// The entire operation (idempotency check, balance update, outbox write)
// happens in a single database transaction.
func (s *Service) Execute(ctx context.Context, idemKey string, req CreateTransferRequest) (*Transfer, error) {
	if req.From == req.To {
		return nil, ErrSameAccount
	}
	if req.Amount <= 0 {
		return nil, errors.New("amount must be positive")
	}

	var transfer *Transfer

	// Circuit breaker wraps the entire DB transaction
	err := s.breaker.Execute(func() error {
		tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback()

		// 1. Acquire idempotency key (SELECT FOR UPDATE)
		key, err := s.idempotency.Acquire(ctx, tx, idemKey)
		if errors.Is(err, idempotency.ErrKeyAlreadyUsed) && key != nil {
			// Return cached response
			if unmarshalErr := json.Unmarshal(key.Response, &transfer); unmarshalErr == nil {
				return nil
			}
			return fmt.Errorf("unmarshal cached response: %w", err)
		}
		if err != nil {
			return fmt.Errorf("acquire idempotency key: %w", err)
		}

		// 2. Check balance and perform transfer
		var balance int64
		err = tx.QueryRowContext(ctx,
			`SELECT balance FROM accounts WHERE id = $1 FOR UPDATE`,
			req.From,
		).Scan(&balance)
		if err != nil {
			s.idempotency.Fail(ctx, tx, idemKey)
			return fmt.Errorf("fetch sender balance: %w", err)
		}

		if balance < req.Amount {
			s.idempotency.Fail(ctx, tx, idemKey)
			return ErrInsufficientBalance
		}

		// Debit sender
		if _, err := tx.ExecContext(ctx,
			`UPDATE accounts SET balance = balance - $1, updated_at = $2 WHERE id = $3`,
			req.Amount, time.Now().UTC(), req.From,
		); err != nil {
			return fmt.Errorf("debit sender %s: %w", req.From, err)
		}

		// Credit receiver
		if _, err := tx.ExecContext(ctx,
			`UPDATE accounts SET balance = balance + $1, updated_at = $2 WHERE id = $3`,
			req.Amount, time.Now().UTC(), req.To,
		); err != nil {
			return fmt.Errorf("credit receiver %s: %w", req.To, err)
		}

		// 3. Create transfer record
		transfer = &Transfer{
			ID:        idemKey,
			From:      req.From,
			To:        req.To,
			Amount:    req.Amount,
			Status:    "completed",
			CreatedAt: time.Now().UTC(),
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO transfers (id, from_account, to_account, amount, status, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			transfer.ID, transfer.From, transfer.To, transfer.Amount, transfer.Status, transfer.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert transfer record: %w", err)
		}

		// 4. Write to outbox (same transaction — guaranteed delivery)
		if err := outbox.Write(ctx, tx, transfer.ID, "transfer.completed", transfer); err != nil {
			return fmt.Errorf("write outbox event: %w", err)
		}

		// 5. Mark idempotency key as completed with cached response
		responseJSON, _ := json.Marshal(transfer)
		if err := s.idempotency.Complete(ctx, tx, idemKey, responseJSON); err != nil {
			return fmt.Errorf("complete idempotency key: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit transfer tx: %w", err)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("transfer.Execute: %w", err)
	}

	return transfer, nil
}
