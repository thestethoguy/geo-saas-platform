package cache

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps *redis.Client and exposes helpers used by the handlers.
type Client struct {
	rdb *redis.Client
}

// NewRedisClient creates and validates a Redis connection from a full URL.
//
//   redisURL – full connection URL, e.g.:
//     "redis://:password@localhost:6379"          (plain-text, local/docker)
//     "rediss://user:password@host:6380"          (TLS — Upstash / Render / Railway)
//
// redis.ParseURL() reads the scheme and enables TLS automatically for "rediss://",
// which fixes the EOF error seen when connecting to Upstash with a plain Options struct.
func NewRedisClient(redisURL string) (*Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("redis: invalid URL: %w", err)
	}

	// Layer our connection-pool tuning on top of what ParseURL provides.
	// ParseURL already sets Addr, Password, DB, and TLSConfig from the URL.
	opts.DialTimeout  = 5 * time.Second
	opts.ReadTimeout  = 3 * time.Second
	opts.WriteTimeout = 3 * time.Second
	opts.PoolSize     = 10
	opts.MinIdleConns = 2

	rdb := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	log.Println("[cache] Redis connection established ✓")
	return &Client{rdb: rdb}, nil
}

// Get fetches a cached value by key. Returns ("", redis.Nil) on a miss.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	return c.rdb.Get(ctx, key).Result()
}

// Set stores a value with the given TTL. A zero TTL means no expiry.
func (c *Client) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return c.rdb.Set(ctx, key, value, ttl).Err()
}

// IncrWindowAndExpire increments a counter key by 1 and, in the same pipeline
// round-trip, sets its TTL. Used by the fixed-window rate limiter where the
// key name already encodes the time window, making EXPIRE idempotent.
// Returns the new counter value after incrementing.
func (c *Client) IncrWindowAndExpire(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	pipe := c.rdb.Pipeline()
	incrCmd := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("redis pipeline error: %w", err)
	}
	return incrCmd.Val(), nil
}

// Close releases the underlying connection pool.
func (c *Client) Close() error {
	return c.rdb.Close()
}
