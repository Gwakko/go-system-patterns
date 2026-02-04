package transfer

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gwakko/go-system-patterns/internal/circuitbreaker"
	"github.com/gwakko/go-system-patterns/internal/idempotency"
	"github.com/gwakko/go-system-patterns/internal/middleware"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) HandleTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	idemKey, _ := r.Context().Value(middleware.IdempotencyKeyCtx).(string)
	if idemKey == "" {
		http.Error(w, `{"error":"idempotency key required"}`, http.StatusBadRequest)
		return
	}

	var req CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	transfer, err := h.service.Execute(r.Context(), idemKey, req)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrInsufficientBalance):
			status = http.StatusUnprocessableEntity
		case errors.Is(err, ErrSameAccount):
			status = http.StatusBadRequest
		case errors.Is(err, idempotency.ErrKeyNotFound):
			status = http.StatusBadRequest
		case errors.Is(err, idempotency.ErrKeyExpired):
			status = http.StatusGone
		case errors.Is(err, idempotency.ErrKeyAlreadyUsed):
			status = http.StatusConflict
		case errors.Is(err, circuitbreaker.ErrCircuitOpen):
			status = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(transfer)
}
