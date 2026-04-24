// Package middleware provides HTTP middleware for the Geo SaaS API.
// This file implements API key authentication with a Redis cache-aside pattern:
//
//  1. Extract the Bearer token from the Authorization header.
//  2. SHA-256 hash the raw token — we never store or log plain-text keys.
//  3. Check Redis ("auth:key:<hash>") for a cached tier string (10-min TTL).
//  4. On a cache miss, query Postgres auth_keys and repopulate Redis.
//  5. Inject the tier and key hash into the request context for downstream use
//     (rate limiter, analytics, etc.).
package middleware

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"geo-saas-platform/internal/cache"
)

// ─────────────────────────────────────────────────────────────────────────────
// Context keys — typed to avoid collisions with other packages
// ─────────────────────────────────────────────────────────────────────────────

type contextKey string

const (
	// TierKey is the request-context key that holds the authenticated tier
	// string ("free" or "premium") for use by the rate limiter.
	TierKey contextKey = "tier"

	// KeyHashKey is the request-context key that holds the hex SHA-256 hash
	// of the validated API key. Used by the rate limiter to build per-key
	// Redis counter names.
	KeyHashKey contextKey = "keyHash"
)

// TierFromCtx extracts the tier from a request context. Returns "" if absent.
func TierFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(TierKey).(string)
	return v
}

// KeyHashFromCtx extracts the key hash from a request context.
func KeyHashFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(KeyHashKey).(string)
	return v
}

// ─────────────────────────────────────────────────────────────────────────────
// Auth middleware
// ─────────────────────────────────────────────────────────────────────────────

// authMiddleware holds the dependencies needed for API key validation.
type authMiddleware struct {
	db    *sql.DB
	cache *cache.Client
}

// NewAuth returns a chi-compatible middleware function that authenticates
// requests using Bearer tokens validated against the auth_keys table.
//
//	db    – Postgres connection pool
//	cache – Redis client
func NewAuth(db *sql.DB, c *cache.Client) func(http.Handler) http.Handler {
	m := &authMiddleware{db: db, cache: c}
	return m.handler
}

func (m *authMiddleware) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ── 1. Extract Bearer token ───────────────────────────────────────
		rawToken := extractBearer(r)
		if rawToken == "" {
			writeErr(w, http.StatusUnauthorized,
				"missing or invalid Authorization header (expected: Bearer <token>)")
			return
		}

		// ── 2. Hash the token — never touch raw keys after this point ─────
		hash := sha256Hex(rawToken)
		cacheKey := "auth:key:" + hash

		// ── 3. Redis cache check ──────────────────────────────────────────
		tier, err := m.cache.Get(r.Context(), cacheKey)
		if err == nil {
			// Cache hit — propagate context values and continue
			ctx := context.WithValue(r.Context(), TierKey, tier)
			ctx = context.WithValue(ctx, KeyHashKey, hash)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		if !errors.Is(err, redis.Nil) {
			// Non-nil, non-miss error from Redis — log and fall through to Postgres
			log.Printf("[auth] redis GET error: %v", err)
		}

		// ── 4. Postgres lookup on cache miss ──────────────────────────────
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		var dbTier string
		err = m.db.QueryRowContext(ctx,
			`SELECT tier FROM auth_keys WHERE key_hash = $1 AND is_active = TRUE`,
			hash,
		).Scan(&dbTier)

		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusUnauthorized, "invalid API key")
			return
		}
		if err != nil {
			log.Printf("[auth] postgres error: %v", err)
			writeErr(w, http.StatusInternalServerError, "authentication service unavailable")
			return
		}

		// ── 5. Populate Redis cache (10-minute TTL) ───────────────────────
		if setErr := m.cache.Set(r.Context(), cacheKey, dbTier, 10*time.Minute); setErr != nil {
			log.Printf("[auth] redis SET error: %v", setErr)
			// Non-fatal — we still have the valid tier from Postgres
		}

		// ── 6. Inject into context and proceed ───────────────────────────
		reqCtx := context.WithValue(r.Context(), TierKey, dbTier)
		reqCtx = context.WithValue(reqCtx, KeyHashKey, hash)
		next.ServeHTTP(w, r.WithContext(reqCtx))
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// extractBearer parses "Authorization: Bearer <token>" and returns the token.
// Returns "" if the header is absent or malformed.
func extractBearer(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return ""
	}
	return token
}

// sha256Hex returns the lowercase hex-encoded SHA-256 digest of s.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum)
}

// writeErr writes a standard JSON error response.
func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
