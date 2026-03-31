package ratelimit

import (
	"testing"
	"time"
)

func TestTokenBucket(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "allows requests when tokens available",
			run: func(t *testing.T) {
				tb := NewTokenBucket(5, 1)
				for i := 0; i < 5; i++ {
					if !tb.Allow() {
						t.Fatalf("request %d should have been allowed", i+1)
					}
				}
			},
		},
		{
			name: "rejects when bucket empty",
			run: func(t *testing.T) {
				tb := NewTokenBucket(2, 0.01) // very slow refill
				tb.Allow()
				tb.Allow()
				if tb.Allow() {
					t.Fatal("expected rejection when bucket is empty")
				}
			},
		},
		{
			name: "refills over time",
			run: func(t *testing.T) {
				tb := NewTokenBucket(2, 200) // 200 tokens/sec = 1 token per 5ms
				tb.Allow()
				tb.Allow()
				if tb.Allow() {
					t.Fatal("should be empty before refill")
				}
				time.Sleep(15 * time.Millisecond)
				if !tb.Allow() {
					t.Fatal("expected token to be available after refill time")
				}
			},
		},
		{
			name: "burst behavior full bucket allows burst",
			run: func(t *testing.T) {
				burst := 10
				tb := NewTokenBucket(float64(burst), 1)
				for i := 0; i < burst; i++ {
					if !tb.Allow() {
						t.Fatalf("burst request %d should have been allowed", i+1)
					}
				}
				if tb.Allow() {
					t.Fatal("should reject after full burst consumed")
				}
			},
		},
		{
			name: "tokens never exceed max",
			run: func(t *testing.T) {
				tb := NewTokenBucket(5, 1000)
				time.Sleep(20 * time.Millisecond) // would refill well beyond max
				tokens := tb.Tokens()
				if tokens > 5 {
					t.Fatalf("tokens %f exceeded max 5", tokens)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}
