package ratelimit

import "sync"

// PerClient maintains a separate token bucket per client identifier (IP, API key, etc).
type PerClient struct {
	mu         sync.Mutex
	buckets    map[string]*TokenBucket
	maxTokens  float64
	refillRate float64
}

func NewPerClient(maxTokens, refillRate float64) *PerClient {
	return &PerClient{
		buckets:    make(map[string]*TokenBucket),
		maxTokens:  maxTokens,
		refillRate: refillRate,
	}
}

func (pc *PerClient) Allow(clientID string) bool {
	pc.mu.Lock()
	bucket, ok := pc.buckets[clientID]
	if !ok {
		bucket = NewTokenBucket(pc.maxTokens, pc.refillRate)
		pc.buckets[clientID] = bucket
	}
	pc.mu.Unlock()

	return bucket.Allow()
}
