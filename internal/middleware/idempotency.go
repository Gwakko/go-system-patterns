package middleware

import (
	"context"
	"net/http"
)

type contextKey string

const IdempotencyKeyCtx contextKey = "idempotency_key"

// RequireIdempotencyKey checks for the Idempotency-Key header on mutating requests.
func RequireIdempotencyKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodPut {
			next.ServeHTTP(w, r)
			return
		}

		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			http.Error(w, `{"error":"Idempotency-Key header is required"}`, http.StatusBadRequest)
			return
		}

		ctx := context.WithValue(r.Context(), IdempotencyKeyCtx, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
