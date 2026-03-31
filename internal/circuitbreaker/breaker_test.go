package circuitbreaker

import (
	"errors"
	"testing"
	"time"
)

var errFail = errors.New("failure")

func TestNew_StartsClosed(t *testing.T) {
	cb := New(3, 1, 5*time.Second)
	if cb.State() != Closed {
		t.Fatalf("expected Closed, got %s", cb.State())
	}
}

func TestCircuitBreaker(t *testing.T) {
	failFn := func() error { return errFail }
	okFn := func() error { return nil }

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "opens after N failures",
			run: func(t *testing.T) {
				cb := New(3, 1, 5*time.Second)
				for i := 0; i < 3; i++ {
					_ = cb.Execute(failFn)
				}
				if cb.State() != Open {
					t.Fatalf("expected Open after 3 failures, got %s", cb.State())
				}
			},
		},
		{
			name: "rejects when open",
			run: func(t *testing.T) {
				cb := New(3, 1, 5*time.Second)
				for i := 0; i < 3; i++ {
					_ = cb.Execute(failFn)
				}
				err := cb.Execute(okFn)
				if !errors.Is(err, ErrCircuitOpen) {
					t.Fatalf("expected ErrCircuitOpen, got %v", err)
				}
			},
		},
		{
			name: "transitions to half-open after timeout",
			run: func(t *testing.T) {
				cb := New(3, 1, 10*time.Millisecond)
				for i := 0; i < 3; i++ {
					_ = cb.Execute(failFn)
				}
				if cb.State() != Open {
					t.Fatalf("expected Open, got %s", cb.State())
				}
				time.Sleep(20 * time.Millisecond)
				// Execute a successful call; the breaker should transition to HalfOpen
				// internally and then process the call.
				_ = cb.Execute(okFn)
				// After one success with successThreshold=1, it should close.
				if cb.State() != Closed {
					t.Fatalf("expected Closed after success in half-open, got %s", cb.State())
				}
			},
		},
		{
			name: "closes after N successes in half-open",
			run: func(t *testing.T) {
				cb := New(3, 3, 10*time.Millisecond)
				for i := 0; i < 3; i++ {
					_ = cb.Execute(failFn)
				}
				time.Sleep(20 * time.Millisecond)

				// Need 3 successes to close
				for i := 0; i < 2; i++ {
					_ = cb.Execute(okFn)
					if cb.State() != HalfOpen {
						t.Fatalf("expected HalfOpen after %d successes, got %s", i+1, cb.State())
					}
				}
				_ = cb.Execute(okFn)
				if cb.State() != Closed {
					t.Fatalf("expected Closed after 3 successes, got %s", cb.State())
				}
			},
		},
		{
			name: "re-opens on failure in half-open",
			run: func(t *testing.T) {
				cb := New(3, 3, 10*time.Millisecond)
				for i := 0; i < 3; i++ {
					_ = cb.Execute(failFn)
				}
				time.Sleep(20 * time.Millisecond)

				// One success puts us in half-open
				_ = cb.Execute(okFn)
				if cb.State() != HalfOpen {
					t.Fatalf("expected HalfOpen, got %s", cb.State())
				}
				// A failure should re-open
				_ = cb.Execute(failFn)
				if cb.State() != Open {
					t.Fatalf("expected Open after failure in half-open, got %s", cb.State())
				}
			},
		},
		{
			name: "success in closed state resets failure count",
			run: func(t *testing.T) {
				cb := New(3, 1, 5*time.Second)
				_ = cb.Execute(failFn)
				_ = cb.Execute(failFn)
				_ = cb.Execute(okFn) // resets failures
				_ = cb.Execute(failFn)
				_ = cb.Execute(failFn)
				if cb.State() != Closed {
					t.Fatalf("expected Closed (failures reset by success), got %s", cb.State())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}
