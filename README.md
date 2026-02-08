# Go System Design Patterns

Transfer service demonstrating common system design patterns in Go. All patterns work together in a single coherent application — not isolated examples.

## Patterns Implemented

### 1. Idempotency Keys

Server generates unique keys (`POST /api/idempotency-keys`). Client includes the key in `Idempotency-Key` header for mutating operations. The key is locked with `SELECT ... FOR UPDATE` in the same transaction as the business operation. Repeated requests with the same key return the cached response instead of executing again.

**Why it matters:** Network retries, client timeouts, and load balancer replays can cause duplicate operations. Idempotency keys guarantee exactly-once semantics at the application level.

### 2. Transactional Outbox

Events are written to the `outbox` table in the same database transaction as the business data. A separate relay process polls unpublished events and publishes them to an external broker.

**Why it matters:** Dual writes (DB + message broker) aren't atomic. If the app writes to the DB but crashes before publishing, the event is lost. The outbox pattern guarantees that if the business data is committed, the event will eventually be published.

**Implementation details:**
- `SELECT ... FOR UPDATE SKIP LOCKED` allows multiple relay instances without conflicts
- Relay runs as a separate process (`cmd/relay/`)
- Publisher interface is pluggable (stdout for demo, swap with Kafka/RabbitMQ)

### 3. Circuit Breaker

Wraps the entire transfer transaction. After 5 consecutive failures → circuit opens (rejects immediately for 30s). After timeout → half-open (allows probe requests). 2 successes in half-open → closes circuit.

**Why it matters:** Prevents cascading failures when the database or downstream service is unhealthy. Fast-fails instead of accumulating connections and timeouts.

### 4. Token Bucket Rate Limiting

Per-client rate limiting using the token bucket algorithm. Each client (identified by IP) gets a bucket with configurable capacity and refill rate.

**Why it matters:** Protects the service from abuse and ensures fair resource distribution. Token bucket is chosen over fixed window because it allows bursts while maintaining average rate.

### 5. Serializable Transactions

Transfer operations use `SERIALIZABLE` isolation level with `SELECT ... FOR UPDATE` on account balances. This prevents phantom reads and ensures balance consistency under concurrent transfers.

**Why it matters:** Without proper isolation, two concurrent transfers from the same account could both read sufficient balance and overdraw.

## Quick Start

```bash
# Run everything
docker compose up -d

# Apply migrations (auto-applied via docker-entrypoint-initdb.d)

# 1. Generate idempotency key
KEY=$(curl -s -X POST http://localhost:8080/api/idempotency-keys | jq -r '.key')

# 2. Execute transfer with the key
curl -X POST http://localhost:8080/api/transfers \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $KEY" \
  -d '{"from":"acc_alice","to":"acc_bob","amount":1000}'

# 3. Retry the same request (returns cached response, no duplicate transfer)
curl -X POST http://localhost:8080/api/transfers \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $KEY" \
  -d '{"from":"acc_alice","to":"acc_bob","amount":1000}'

# 4. Check health + circuit breaker state
curl http://localhost:8080/api/health
```

## Architecture

```
cmd/
├── server/         # HTTP API server
└── relay/          # Outbox relay (separate process)

internal/
├── idempotency/    # Key generation, acquire/complete/fail lifecycle
├── outbox/         # Transactional write + polling relay
├── transfer/       # Business logic: service + HTTP handler
├── circuitbreaker/ # State machine: closed → open → half-open
├── ratelimit/      # Token bucket + per-client wrapper
└── middleware/      # HTTP middleware: rate limit, idempotency check

migrations/
└── 001_init.sql    # Schema + seed data
```

**Request flow:**
```
Client → Rate Limiter → Idempotency Check → Handler
                                               │
                              ┌────────────────┤
                              ▼                ▼
                       Circuit Breaker    Idempotency
                              │           Acquire (FOR UPDATE)
                              ▼
                       BEGIN SERIALIZABLE
                         ├─ Check balance (FOR UPDATE)
                         ├─ Debit sender
                         ├─ Credit receiver
                         ├─ Insert transfer record
                         ├─ Write outbox event
                         ├─ Complete idempotency key
                         └─ COMMIT
                              │
                              ▼
                       Relay (async) → Publisher → Broker
```

## API

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/idempotency-keys` | Generate new idempotency key (24h TTL) |
| POST | `/api/transfers` | Execute transfer (requires `Idempotency-Key` header) |
| GET | `/api/health` | Health check + circuit breaker state |

## Error Responses

| Status | Meaning |
|--------|---------|
| 409 Conflict | Idempotency key already used (returns cached response) |
| 410 Gone | Idempotency key expired |
| 422 Unprocessable | Insufficient balance |
| 429 Too Many Requests | Rate limit exceeded |
| 503 Service Unavailable | Circuit breaker is open |

## TODO

- [ ] Redis-backed rate limiter (for multi-instance)
- [ ] Kafka/RabbitMQ publisher implementation
- [ ] Graceful shutdown with in-flight request draining
- [ ] Prometheus metrics (transfer count, latency, circuit state)
- [ ] Dead letter handling for failed outbox events
- [ ] Account API (create, get balance, transaction history)
