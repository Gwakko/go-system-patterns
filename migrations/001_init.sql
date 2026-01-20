CREATE TABLE IF NOT EXISTS idempotency_keys (
    key         VARCHAR(64) PRIMARY KEY,
    status      VARCHAR(20) NOT NULL DEFAULT 'created',
    response    BYTEA,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_idem_expires ON idempotency_keys (expires_at);

CREATE TABLE IF NOT EXISTS accounts (
    id         VARCHAR(64) PRIMARY KEY,
    balance    BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS transfers (
    id           VARCHAR(64) PRIMARY KEY,
    from_account VARCHAR(64) NOT NULL REFERENCES accounts(id),
    to_account   VARCHAR(64) NOT NULL REFERENCES accounts(id),
    amount       BIGINT NOT NULL CHECK (amount > 0),
    status       VARCHAR(20) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_transfers_from ON transfers (from_account, created_at DESC);
CREATE INDEX idx_transfers_to   ON transfers (to_account, created_at DESC);

CREATE TABLE IF NOT EXISTS outbox (
    id           VARCHAR(64) PRIMARY KEY,
    aggregate_id VARCHAR(64) NOT NULL,
    event_type   VARCHAR(100) NOT NULL,
    payload      JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ
);

CREATE INDEX idx_outbox_unpublished ON outbox (created_at ASC) WHERE published_at IS NULL;

-- Seed test accounts
INSERT INTO accounts (id, balance) VALUES
    ('acc_alice', 100000),
    ('acc_bob', 50000)
ON CONFLICT DO NOTHING;
