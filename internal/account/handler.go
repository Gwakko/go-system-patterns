package account

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Account struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Transaction struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Amount    int64     `json:"amount"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type Handler struct {
	db *sql.DB
}

func NewHandler(db *sql.DB) *Handler {
	return &Handler{db: db}
}

// HandleGetAccount handles GET /api/accounts/{id}
func (h *Handler) HandleGetAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	id := extractAccountID(r.URL.Path, "/api/accounts/")
	if id == "" {
		http.Error(w, `{"error":"account id required"}`, http.StatusBadRequest)
		return
	}

	// Reject paths that look like sub-resources (e.g. /transactions)
	if strings.Contains(id, "/") {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	var acct Account
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id, balance, created_at, updated_at FROM accounts WHERE id = $1`, id,
	).Scan(&acct.ID, &acct.Balance, &acct.CreatedAt, &acct.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"account not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, fmt.Errorf("fetch account: %w", err)), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(acct)
}

// HandleGetTransactions handles GET /api/accounts/{id}/transactions
func (h *Handler) HandleGetTransactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Extract account ID from path: /api/accounts/{id}/transactions
	path := strings.TrimPrefix(r.URL.Path, "/api/accounts/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[0] == "" || parts[1] != "transactions" {
		http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
		return
	}
	id := parts[0]

	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, from_account, to_account, amount, status, created_at
		 FROM transfers
		 WHERE from_account = $1 OR to_account = $1
		 ORDER BY created_at DESC`, id,
	)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, fmt.Errorf("query transactions: %w", err)), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	transactions := make([]Transaction, 0)
	for rows.Next() {
		var t Transaction
		if err := rows.Scan(&t.ID, &t.From, &t.To, &t.Amount, &t.Status, &t.CreatedAt); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, fmt.Errorf("scan transaction: %w", err)), http.StatusInternalServerError)
			return
		}
		transactions = append(transactions, t)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, fmt.Errorf("iterate transactions: %w", err)), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(transactions)
}

// extractAccountID extracts the account ID segment from the URL path.
func extractAccountID(path, prefix string) string {
	return strings.TrimPrefix(path, prefix)
}
