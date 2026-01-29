package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit breaker is open")

type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Breaker implements the circuit breaker pattern.
// Closed → Open after failureThreshold consecutive failures.
// Open → HalfOpen after resetTimeout.
// HalfOpen → Closed on success, Open on failure.
type Breaker struct {
	mu               sync.Mutex
	state            State
	failures         int
	successes        int
	failureThreshold int
	successThreshold int // successes needed in half-open to close
	resetTimeout     time.Duration
	lastFailure      time.Time
}

func New(failureThreshold, successThreshold int, resetTimeout time.Duration) *Breaker {
	return &Breaker{
		state:            Closed,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		resetTimeout:     resetTimeout,
	}
}

// Execute runs the given function through the circuit breaker.
func (b *Breaker) Execute(fn func() error) error {
	b.mu.Lock()

	switch b.state {
	case Open:
		if time.Since(b.lastFailure) > b.resetTimeout {
			b.state = HalfOpen
			b.successes = 0
			b.mu.Unlock()
		} else {
			b.mu.Unlock()
			return ErrCircuitOpen
		}
	default:
		b.mu.Unlock()
	}

	err := fn()

	b.mu.Lock()
	defer b.mu.Unlock()

	if err != nil {
		b.failures++
		b.lastFailure = time.Now()

		if b.state == HalfOpen || b.failures >= b.failureThreshold {
			b.state = Open
			b.failures = 0
		}
		return err
	}

	if b.state == HalfOpen {
		b.successes++
		if b.successes >= b.successThreshold {
			b.state = Closed
			b.failures = 0
		}
	} else {
		b.failures = 0
	}

	return nil
}

func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}
