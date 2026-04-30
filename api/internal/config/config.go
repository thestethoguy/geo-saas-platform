package config

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration, populated from environment variables.
type Config struct {
	// App
	AppPort  string
	AppEnv   string
	LogLevel string

	// PostgreSQL
	PostgresDSN string

	// Redis — full connection URL (redis:// or rediss:// for TLS).
	// Passed verbatim to redis.ParseURL() so TLS is inherited from the scheme.
	RedisURL string

	// Typesense — use TypesenseURL field directly; Host/Port kept for reference
	TypesenseServerURL string
	TypesenseAPIKey    string
}

// Load reads the .env file (if present) and populates Config.
// Env variables already set in the shell always take precedence over .env.
func Load() *Config {
	// Load .env from the project root (one level up from /api).
	// This is a no-op in production — cloud providers inject env vars directly.
	if err := godotenv.Load("../.env"); err != nil {
		log.Println("[config] .env file not found, relying on environment variables")
	}

	tsURL, tsKey := buildTypesenseConfig()

	cfg := &Config{
		AppPort:  getEnv("APP_PORT", "8080"),
		AppEnv:   getEnv("APP_ENV", "development"),
		LogLevel: getEnv("LOG_LEVEL", "info"),

		PostgresDSN: buildPostgresDSN(),

		RedisURL: buildRedisURL(),

		TypesenseServerURL: tsURL,
		TypesenseAPIKey:    tsKey,
	}

	return cfg
}

// TypesenseURL returns the resolved Typesense server URL.
// Use this instead of building the URL manually.
func (c *Config) TypesenseURL() string {
	return c.TypesenseServerURL
}

// buildPostgresDSN returns a libpq-style DSN.
//
// Priority (highest → lowest):
//  1. DATABASE_URL  – single connection string (Render / Railway / Heroku standard)
//  2. DB_URL        – alias used by some Render service configurations
//  3. Individual    – POSTGRES_HOST / POSTGRES_PORT / POSTGRES_USER / …
func buildPostgresDSN() string {
	// Check for a full connection URL first (cloud providers)
	if v := getEnv("DATABASE_URL", ""); v != "" {
		log.Println("[config] PostgreSQL: using DATABASE_URL")
		return v
	}
	if v := getEnv("DB_URL", ""); v != "" {
		log.Println("[config] PostgreSQL: using DB_URL")
		return v
	}

	// Fall back to individual variables (local / docker-compose)
	host    := getEnv("POSTGRES_HOST", "localhost")
	port    := getEnvInt("POSTGRES_PORT", 5432)
	user    := getEnv("POSTGRES_USER", "geouser")
	password := getEnv("POSTGRES_PASSWORD", "geopassword")
	dbname  := getEnv("POSTGRES_DB", "geosaas")
	sslmode := getEnv("POSTGRES_SSLMODE", "disable")

	log.Printf("[config] PostgreSQL: connecting to %s:%d/%s", host, port, dbname)
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode,
	)
}

// buildTypesenseConfig returns the server URL and API key for Typesense.
//
// Priority (highest → lowest):
//  1. TYPESENSE_URL      – full URL incl. scheme, e.g. "https://host:443" (Render / self-hosted)
//  2. Individual vars    – TYPESENSE_PROTOCOL + TYPESENSE_HOST + TYPESENSE_PORT
//
// TYPESENSE_PROTOCOL defaults to "http" for local/docker-compose.
// Set it to "https" in production when the provider terminates TLS.
func buildTypesenseConfig() (serverURL string, apiKey string) {
	apiKey = os.Getenv("TYPESENSE_API_KEY") // explicit — never silently empty

	// 1. Full URL var — already carries its own scheme; use verbatim
	if v := os.Getenv("TYPESENSE_URL"); v != "" {
		log.Printf("[config] Typesense: using TYPESENSE_URL → %s", v)
		return v, apiKey
	}

	// 2. Build from individual vars
	protocol := os.Getenv("TYPESENSE_PROTOCOL")
	if protocol == "" {
		protocol = "http" // safe default for local / docker-compose
	}

	host := os.Getenv("TYPESENSE_HOST")
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("TYPESENSE_PORT")
	if port == "" {
		port = "8108"
	}

	built := fmt.Sprintf("%s://%s:%s", protocol, host, port)
	log.Printf("[config] Typesense: protocol=%q host=%q port=%q → %s", protocol, host, port, built)
	return built, apiKey
}

// buildRedisURL returns a full Redis connection URL for redis.ParseURL().
//
// Priority (highest → lowest):
//  1. REDIS_URL  – passed verbatim; supports redis:// and rediss:// (TLS).
//                  Upstash, Render, Railway and similar providers set this.
//  2. Individual – REDIS_HOST / REDIS_PORT / REDIS_PASSWORD → redis://:pass@host:port
//
// The URL is handed directly to redis.ParseURL() in the cache package, which
// automatically enables TLS when the scheme is "rediss".
func buildRedisURL() string {
	if raw := getEnv("REDIS_URL", ""); raw != "" {
		log.Printf("[config] Redis: using REDIS_URL (scheme preserved for TLS detection)")
		return raw
	}

	// Fall back to individual variables (local / docker-compose)
	host     := getEnv("REDIS_HOST",     "localhost")
	port     := getEnv("REDIS_PORT",     "6379")
	password := getEnv("REDIS_PASSWORD", "")
	log.Printf("[config] Redis: building URL from individual vars → %s:%s", host, port)
	if password != "" {
		return fmt.Sprintf("redis://:%s@%s:%s", password, host, port)
	}
	return fmt.Sprintf("redis://%s:%s", host, port)
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
