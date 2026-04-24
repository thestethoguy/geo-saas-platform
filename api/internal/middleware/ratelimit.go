// Package middleware — rate limiting.
//
// Strategy: Fixed Window per API key.
//
// Redis key format:  rl:<keyHash>:<minuteEpoch>
//   where minuteEpoch = Unix timestamp / 60  (changes every 60 seconds)
//
// On every request we pipeline INCR + EXPIRE (window TTL = 70s — a 10-second
// buffer past the window boundary to handle clock skew and in-flight requests).
// Because the key name encodes the current minute window, old windows naturally
// disappear once their Redis TTL fires.
//
// Limits:
//   free    → 5   req / minute
//   premium → 100 req / minute
package middleware

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"geo-saas-platform/internal/cache"
)

// tierLimits maps tier name → maximum requests per fixed 60-second window.
var tierLimits = map[string]int64{
	"free":    5,
	"premium": 100,
}

// windowTTL is how long the Redis counter key lives. Set to 70s (60s window +
// 10s buffer) so in-flight requests that straddle the boundary are still counted
// in the correct window rather than triggering a phantom fresh window.
const windowTTL = 70 * time.Second

// rateLimitMiddleware holds the Redis client needed to manage counters.
type rateLimitMiddleware struct {
	cache *cache.Client
}

// NewRateLimit returns a chi-compatible middleware function that enforces
// per-API-key fixed-window rate limits based on the tier injected by NewAuth.
//
// IMPORTANT: NewAuth MUST run before NewRateLimit in the middleware chain.
func NewRateLimit(c *cache.Client) func(http.Handler) http.Handler {
	m := &rateLimitMiddleware{cache: c}
	return m.handler
}

func (m *rateLimitMiddleware) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ── 1. Retrieve identity from context (set by auth middleware) ─────
		tier    := TierFromCtx(r.Context())
		keyHash := KeyHashFromCtx(r.Context())

		if tier == "" || keyHash == "" {
			// Should never happen if middleware order is correct, but guard anyway
			log.Printf("[ratelimit] context missing tier/keyHash — skipping limit")
			next.ServeHTTP(w, r)
			return
		}

		// ── 2. Resolve the per-tier limit ─────────────────────────────────
		limit, ok := tierLimits[tier]
		if !ok {
			limit = tierLimits["free"] // unknown tiers are treated as free
		}

		// ── 3. Build the window key ───────────────────────────────────────
		// minuteEpoch changes every 60 seconds, giving us the fixed window.
		minuteEpoch := time.Now().Unix() / 60
		rlKey := fmt.Sprintf("rl:%s:%d", keyHash, minuteEpoch)

		// ── 4. Increment counter in Redis (pipelined INCR + EXPIRE) ───────
		count, err := m.cache.IncrWindowAndExpire(r.Context(), rlKey, windowTTL)
		if err != nil {
			// Redis error — fail open: log and allow the request through
			log.Printf("[ratelimit] redis error for key %s: %v — allowing request", rlKey, err)
			next.ServeHTTP(w, r)
			return
		}

		// ── 5. Set informational headers on every response ────────────────
		remaining := limit - count
		if remaining < 0 {
			remaining = 0
		}
		w.Header().Set("X-RateLimit-Limit",     strconv.FormatInt(limit, 10))
		w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))
		w.Header().Set("X-RateLimit-Reset",     strconv.FormatInt((minuteEpoch+1)*60, 10))

		// ── 6. Enforce the limit ──────────────────────────────────────────
		if count > limit {
			w.Header().Set("Retry-After", "60")
			writeErr(w, http.StatusTooManyRequests,
				fmt.Sprintf("rate limit exceeded: %d requests per minute allowed for '%s' tier",
					limit, tier))
			return
		}

		next.ServeHTTP(w, r)
	})
}
