-- =============================================================================
--  GEO SAAS PLATFORM — Initial Schema
--  Migration: 001_init_schema.sql
--  Hierarchy: State -> District -> Sub-District -> Village
--  Design Goals:
--    • lgd_code as the authoritative external identifier (India's LGD system)
--    • Composite unique constraints to ensure data integrity
--    • GIN index on village name for fast ILIKE/trigram search
--    • Separate api_keys + plans tables for SaaS auth (scaffolded here)
-- =============================================================================

-- Enable trigram extension for fast partial-text search on village names
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ─────────────────────────────────────────────
-- 1. STATES
-- ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS states (
    id          SERIAL          PRIMARY KEY,
    lgd_code    VARCHAR(10)     NOT NULL UNIQUE,   -- e.g. "01" for J&K
    name        VARCHAR(100)    NOT NULL,
    name_hi     VARCHAR(100),                      -- Hindi name (optional)
    is_active   BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_states_lgd_code ON states (lgd_code);
CREATE INDEX idx_states_name     ON states (name);

-- ─────────────────────────────────────────────
-- 2. DISTRICTS
-- ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS districts (
    id          SERIAL          PRIMARY KEY,
    lgd_code    VARCHAR(10)     NOT NULL UNIQUE,   -- e.g. "001"
    state_id    INT             NOT NULL REFERENCES states(id) ON DELETE RESTRICT,
    name        VARCHAR(150)    NOT NULL,
    name_hi     VARCHAR(150),
    is_active   BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_districts_lgd_code ON districts (lgd_code);
CREATE INDEX idx_districts_state_id ON districts (state_id);
CREATE INDEX idx_districts_name     ON districts (name);

-- ─────────────────────────────────────────────
-- 3. SUB-DISTRICTS (Talukas / Tehsils / Blocks)
-- ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS sub_districts (
    id              SERIAL          PRIMARY KEY,
    lgd_code        VARCHAR(10)     NOT NULL UNIQUE,
    district_id     INT             NOT NULL REFERENCES districts(id) ON DELETE RESTRICT,
    name            VARCHAR(150)    NOT NULL,
    name_hi         VARCHAR(150),
    is_active       BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sub_districts_lgd_code    ON sub_districts (lgd_code);
CREATE INDEX idx_sub_districts_district_id ON sub_districts (district_id);
CREATE INDEX idx_sub_districts_name        ON sub_districts (name);

-- ─────────────────────────────────────────────
-- 4. VILLAGES (Core dataset — largest table)
-- ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS villages (
    id                  SERIAL          PRIMARY KEY,
    lgd_code            VARCHAR(15)     NOT NULL UNIQUE,
    sub_district_id     INT             NOT NULL REFERENCES sub_districts(id) ON DELETE RESTRICT,
    name                VARCHAR(200)    NOT NULL,
    name_hi             VARCHAR(200),
    census_code         VARCHAR(20),               -- Census 2011 village code (optional)
    pincode             VARCHAR(6),                -- Postal PIN code (optional)
    latitude            NUMERIC(10, 7),
    longitude           NUMERIC(10, 7),
    population          INT,
    is_active           BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

-- Standard lookups
CREATE INDEX idx_villages_lgd_code         ON villages (lgd_code);
CREATE INDEX idx_villages_sub_district_id  ON villages (sub_district_id);

-- Trigram index for fast ILIKE autocomplete: WHERE name ILIKE '%query%'
CREATE INDEX idx_villages_name_trgm ON villages USING GIN (name gin_trgm_ops);

-- Optional: covering index for the most common API query pattern
-- (sub_district_id filter + name ordering)
CREATE INDEX idx_villages_sub_district_name ON villages (sub_district_id, name);

-- ─────────────────────────────────────────────
-- 5. SUBSCRIPTION PLANS (SaaS tiers)
-- ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS plans (
    id                  SERIAL          PRIMARY KEY,
    name                VARCHAR(50)     NOT NULL UNIQUE,    -- 'free', 'premium', 'pro', 'unlimited'
    daily_req_limit     INT             NOT NULL,           -- -1 = unlimited
    monthly_req_limit   INT             NOT NULL,           -- -1 = unlimited
    rate_limit_rpm      INT             NOT NULL,           -- requests per minute
    price_monthly_inr   NUMERIC(10,2)   NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

-- Seed default plans
INSERT INTO plans (name, daily_req_limit, monthly_req_limit, rate_limit_rpm, price_monthly_inr) VALUES
    ('free',      1000,     10000,    10,  0.00),
    ('premium',   10000,    200000,   60,  999.00),
    ('pro',       100000,   2000000,  300, 4999.00),
    ('unlimited', -1,       -1,       -1,  19999.00)
ON CONFLICT (name) DO NOTHING;

-- ─────────────────────────────────────────────
-- 6. CLIENTS (B2B API consumers)
-- ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS clients (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    company_name    VARCHAR(200)    NOT NULL,
    email           VARCHAR(255)    NOT NULL UNIQUE,
    plan_id         INT             NOT NULL REFERENCES plans(id),
    is_active       BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_clients_email ON clients (email);

-- ─────────────────────────────────────────────
-- 7. API KEYS
-- ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS api_keys (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       UUID            NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    key_hash        VARCHAR(64)     NOT NULL UNIQUE,  -- SHA-256 hash of the raw key
    key_prefix      VARCHAR(8)      NOT NULL,         -- First 8 chars for display (e.g. "gsk_a1b2")
    label           VARCHAR(100),                     -- e.g. "Production", "Staging"
    last_used_at    TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ,                      -- NULL = no expiry
    is_active       BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_api_keys_key_hash  ON api_keys (key_hash);
CREATE INDEX idx_api_keys_client_id ON api_keys (client_id);

-- ─────────────────────────────────────────────
-- 8. REQUEST LOGS (Usage analytics)
-- ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS request_logs (
    id              BIGSERIAL       PRIMARY KEY,
    api_key_id      UUID            REFERENCES api_keys(id) ON DELETE SET NULL,
    client_id       UUID            REFERENCES clients(id) ON DELETE SET NULL,
    endpoint        VARCHAR(100)    NOT NULL,
    method          VARCHAR(10)     NOT NULL,
    status_code     INT             NOT NULL,
    latency_ms      INT,
    ip_address      INET,
    logged_at       TIMESTAMPTZ     NOT NULL DEFAULT NOW()
) PARTITION BY RANGE (logged_at);

-- Create partitions: one per month (add more as needed)
CREATE TABLE request_logs_2025_01 PARTITION OF request_logs
    FOR VALUES FROM ('2025-01-01') TO ('2025-02-01');
CREATE TABLE request_logs_2025_02 PARTITION OF request_logs
    FOR VALUES FROM ('2025-02-01') TO ('2025-03-01');
CREATE TABLE request_logs_2026_01 PARTITION OF request_logs
    FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
CREATE TABLE request_logs_default  PARTITION OF request_logs DEFAULT;

CREATE INDEX idx_req_logs_client_id ON request_logs (client_id, logged_at DESC);
CREATE INDEX idx_req_logs_api_key   ON request_logs (api_key_id, logged_at DESC);

-- ─────────────────────────────────────────────
-- 9. updated_at trigger helper
-- ─────────────────────────────────────────────
CREATE OR REPLACE FUNCTION trigger_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Attach trigger to mutable tables
DO $$
DECLARE
    t TEXT;
BEGIN
    FOREACH t IN ARRAY ARRAY['states','districts','sub_districts','villages','clients']
    LOOP
        EXECUTE format(
            'CREATE TRIGGER set_updated_at BEFORE UPDATE ON %I
             FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at()', t);
    END LOOP;
END;
$$;
