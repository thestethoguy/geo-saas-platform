-- =============================================================================
--  GEO SAAS PLATFORM — Migration 002: Auth Keys for SaaS Middleware
--  Table: auth_keys
--  Purpose:
--    A lightweight, self-contained key store used by the API auth middleware.
--    Intentionally simpler than the full api_keys/clients/plans schema
--    (migration 001) — this is the MVP tier that the Phase 5 middleware reads.
--
--  Tiers:
--    'free'    → 5 requests per minute
--    'premium' → 100 requests per minute
-- =============================================================================

CREATE TABLE IF NOT EXISTS auth_keys (
    id          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    key_hash    VARCHAR(64)     NOT NULL UNIQUE,  -- hex-encoded SHA-256 of the raw key
    tier        VARCHAR(20)     NOT NULL DEFAULT 'free'
                                CHECK (tier IN ('free', 'premium')),
    label       VARCHAR(100),                     -- human-readable label (e.g. "Test Free Key")
    is_active   BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

-- Fast lookup path: the middleware queries by key_hash on every cold-cache request
CREATE INDEX IF NOT EXISTS idx_auth_keys_hash ON auth_keys (key_hash) WHERE is_active = TRUE;
