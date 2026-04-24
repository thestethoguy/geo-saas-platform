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
	AppPort string
	AppEnv  string
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
	// Load .env from the project root (one level up from /api)
	if err := godotenv.Load("../.env"); err != nil {
		// Not a fatal error — env vars may be injected via docker/CI
		log.Println("[config] .env file not found, relying on environment variables")
	}

	cfg := &Config{
		AppPort:  getEnv("APP_PORT", "8080"),
		AppEnv:   getEnv("APP_ENV", "development"),
		LogLevel: getEnv("LOG_LEVEL", "info"),

		PostgresDSN: buildPostgresDSN(),

		RedisAddr:     fmt.Sprintf("%s:%s", getEnv("REDIS_HOST", "localhost"), getEnv("REDIS_PORT", "6379")),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),

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

// buildPostgresDSN assembles the Postgres connection string.
func buildPostgresDSN() string {
	host     := getEnv("POSTGRES_HOST", "localhost")
	port     := getEnvInt("POSTGRES_PORT", 5432)
	user     := getEnv("POSTGRES_USER", "geouser")
	password := getEnv("POSTGRES_PASSWORD", "geopassword")
	dbname   := getEnv("POSTGRES_DB", "geosaas")
	sslmode  := getEnv("POSTGRES_SSLMODE", "disable")

	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode,
	)
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
