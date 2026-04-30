-- =============================================================================
--  GEO SAAS PLATFORM — Migration 003: V1 Production Schema
--  Replaces the V0 integer-PK geo tables with UUID-PK equivalents.
--
--  What changes:
--    • Primary keys: SERIAL int  →  UUID (gen_random_uuid())
--    • Foreign keys: int refs    →  UUID refs
--    • Removed: name_hi, is_active, census_code, pincode,
--               latitude, longitude, population
--    • Kept intact: plans, clients, api_keys, auth_keys, request_logs
--
--  Idempotent: safe to re-run on a fresh database (uses IF EXISTS / IF NOT EXISTS).
--  Run order: must run AFTER 001_init_schema.sql and 002_auth_keys.sql.
-- =============================================================================

-- ── 0. Enable uuid-ossp extension (idempotent) ────────────────────────────────
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ── 1. Drop old V0 geo tables (reverse FK order) ─────────────────────────────
-- Safe even if they don't exist yet (fresh install scenario).
DROP TABLE IF EXISTS villages      CASCADE;
DROP TABLE IF EXISTS sub_districts CASCADE;
DROP TABLE IF EXISTS districts     CASCADE;
DROP TABLE IF EXISTS states        CASCADE;

-- ── 2. STATES ─────────────────────────────────────────────────────────────────
CREATE TABLE states (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(150) NOT NULL,
    lgd_code    VARCHAR(10)  NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_v1_states_lgd_code ON states (lgd_code);
CREATE INDEX idx_v1_states_name     ON states (name);

-- ── 3. DISTRICTS ──────────────────────────────────────────────────────────────
CREATE TABLE districts (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    state_id    UUID         NOT NULL REFERENCES states(id) ON DELETE RESTRICT,
    name        VARCHAR(200) NOT NULL,
    lgd_code    VARCHAR(10)  NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_v1_districts_state_id ON districts (state_id);
CREATE INDEX idx_v1_districts_lgd_code ON districts (lgd_code);
CREATE INDEX idx_v1_districts_name     ON districts (name);

-- ── 4. SUB-DISTRICTS ──────────────────────────────────────────────────────────
CREATE TABLE sub_districts (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    district_id     UUID         NOT NULL REFERENCES districts(id) ON DELETE RESTRICT,
    name            VARCHAR(200) NOT NULL,
    lgd_code        VARCHAR(10)  NOT NULL UNIQUE,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_v1_sub_districts_district_id ON sub_districts (district_id);
CREATE INDEX idx_v1_sub_districts_lgd_code    ON sub_districts (lgd_code);
CREATE INDEX idx_v1_sub_districts_name        ON sub_districts (name);

-- ── 5. VILLAGES ───────────────────────────────────────────────────────────────
-- GIN trigram index on name allows fast ILIKE autocomplete from Postgres directly
-- (useful for fallback search if Typesense is unavailable).
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE villages (
    id                  UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    sub_district_id     UUID         NOT NULL REFERENCES sub_districts(id) ON DELETE RESTRICT,
    name                VARCHAR(255) NOT NULL,
    lgd_code            VARCHAR(15)  NOT NULL UNIQUE,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_v1_villages_sub_district_id ON villages (sub_district_id);
CREATE INDEX idx_v1_villages_lgd_code        ON villages (lgd_code);

-- Trigram GIN for fast partial-string search on village name
CREATE INDEX idx_v1_villages_name_trgm
    ON villages USING GIN (name gin_trgm_ops);

-- Covering index: the most common API query pattern is
-- "give me all villages for sub_district X, sorted by name"
CREATE INDEX idx_v1_villages_sub_district_name
    ON villages (sub_district_id, name);
