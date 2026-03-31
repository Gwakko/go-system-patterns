package ratelimit

import (
	"testing"
)

func TestPerClient(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "separate limits per client ID",
			run: func(t *testing.T) {
				pc := NewPerClient(2, 0.01) // 2 tokens, very slow refill

				// Drain client A
				pc.Allow("clientA")
				pc.Allow("clientA")
				if pc.Allow("clientA") {
					t.Fatal("clientA should be rate-limited")
				}

				// Client B should still have tokens
				if !pc.Allow("clientB") {
					t.Fatal("clientB should have its own full bucket")
				}
			},
		},
		{
			name: "new client gets full bucket",
			run: func(t *testing.T) {
				pc := NewPerClient(5, 1)

				for i := 0; i < 5; i++ {
					if !pc.Allow("new-client") {
						t.Fatalf("new client request %d should be allowed", i+1)
					}
				}
			},
		},
		{
			name: "many clients are independent",
			run: func(t *testing.T) {
				pc := NewPerClient(1, 0.01)

				clients := []string{"alpha", "beta", "gamma", "delta"}
				for _, c := range clients {
					if !pc.Allow(c) {
						t.Fatalf("first request for %s should be allowed", c)
					}
				}
				// All should now be exhausted
				for _, c := range clients {
					if pc.Allow(c) {
						t.Fatalf("%s should be rate-limited after 1 request", c)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}
