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

// NewRedisClient creates and validates a Redis connection.
// addr  – "host:port"
// pass  – REDIS_PASSWORD (empty string = no auth)
func NewRedisClient(addr, pass string) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     pass,
		DB:           0, // default database
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 2,
	})

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
