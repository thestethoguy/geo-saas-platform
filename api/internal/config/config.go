package config

import (
	"fmt"
	"log"
	"net/url"
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

	// Redis
	RedisAddr     string
	RedisPassword string

	// Typesense
	TypesenseHost   string
	TypesensePort   string
	TypesenseAPIKey string
}

// Load reads the .env file (if present) and populates Config.
// Env variables already set in the shell always take precedence over .env.
func Load() *Config {
	// Load .env from the project root (one level up from /api).
	// This is a no-op in production — cloud providers inject env vars directly.
	if err := godotenv.Load("../.env"); err != nil {
		log.Println("[config] .env file not found, relying on environment variables")
	}

	redisAddr, redisPwd := buildRedisConfig()

	cfg := &Config{
		AppPort:  getEnv("APP_PORT", "8080"),
		AppEnv:   getEnv("APP_ENV", "development"),
		LogLevel: getEnv("LOG_LEVEL", "info"),

		PostgresDSN: buildPostgresDSN(),

		RedisAddr:     redisAddr,
		RedisPassword: redisPwd,

		TypesenseHost:   getEnv("TYPESENSE_HOST", "localhost"),
		TypesensePort:   getEnv("TYPESENSE_PORT", "8108"),
		TypesenseAPIKey: getEnv("TYPESENSE_API_KEY", ""),
	}

	return cfg
}

// TypesenseURL returns the base URL for the Typesense server.
func (c *Config) TypesenseURL() string {
	return fmt.Sprintf("http://%s:%s", c.TypesenseHost, c.TypesensePort)
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

// buildRedisConfig returns (addr, password) for go-redis.
//
// Priority (highest → lowest):
//  1. REDIS_URL  – redis://[:password@]host[:port][/db] (Render / Upstash / Railway)
//  2. Individual – REDIS_HOST / REDIS_PORT / REDIS_PASSWORD
func buildRedisConfig() (addr string, password string) {
	if raw := getEnv("REDIS_URL", ""); raw != "" {
		u, err := url.Parse(raw)
		if err != nil {
			log.Printf("[config] Redis: failed to parse REDIS_URL (%v) — falling back to individual vars", err)
		} else {
			host := u.Hostname()
			port := u.Port()
			if port == "" {
				port = "6379"
			}
			if u.User != nil {
				password, _ = u.User.Password()
			}
			log.Printf("[config] Redis: using REDIS_URL → %s:%s", host, port)
			return host + ":" + port, password
		}
	}

	// Fall back to individual variables (local / docker-compose)
	host := getEnv("REDIS_HOST", "localhost")
	port := getEnv("REDIS_PORT", "6379")
	log.Printf("[config] Redis: connecting to %s:%s", host, port)
	return host + ":" + port, getEnv("REDIS_PASSWORD", "")
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
