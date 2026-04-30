// seed_keys is a standalone utility script that:
//  1. Generates one cryptographically-random Free key and one Premium key.
//  2. SHA-256 hashes each key.
//  3. Upserts both into the auth_keys table (safe to run multiple times).
//  4. Prints the plain-text keys to stdout so you can copy them for testing.
//
// Usage:
//
//	go run main.go                        # reads .env from project root
//	POSTGRES_HOST=prod-host go run main.go
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

// keySpec describes a key we want to generate and insert.
type keySpec struct {
	label string
	tier  string
}

var keysToSeed = []keySpec{
	{label: "Test Free Key",    tier: "free"},
	{label: "Test Premium Key", tier: "premium"},
}

func main() {
	log.SetFlags(log.Ltime | log.Lmsgprefix)
	log.SetPrefix("[seed_keys] ")

	// ── 1. Load .env from project root ────────────────────────────────────
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	if err := godotenv.Load(filepath.Join(projectRoot, ".env")); err != nil {
		log.Println("WARN: .env not found — relying on environment variables")
	}

	// ── 2. Connect to Postgres ────────────────────────────────────────────
	// Priority: DATABASE_URL (Neon / Render / Railway set this with sslmode already embedded)
	//           → individual POSTGRES_* vars as fallback (local/docker).
	var dsn string
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		log.Println("PostgreSQL: using DATABASE_URL")
		dsn = dbURL
	} else {
		dsn = fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=require",
			getEnv("POSTGRES_HOST", "localhost"),
			getEnv("POSTGRES_PORT", "5432"),
			getEnv("POSTGRES_USER", "geouser"),
			getEnv("POSTGRES_PASSWORD", "geopassword"),
			getEnv("POSTGRES_DB", "geosaas"),
		)
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("FATAL: sql.Open: %v", err)
	}
	defer db.Close()

	for i := 1; i <= 5; i++ {
		if err = db.Ping(); err == nil {
			break
		}
		log.Printf("Postgres not ready (attempt %d/5) — retrying in 2s", i)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("FATAL: Postgres unreachable: %v", err)
	}
	log.Println("PostgreSQL connected ✓")

	// ── 3. Ensure the auth_keys table exists ─────────────────────────────
	// (The migration file is run automatically by docker-compose on first start,
	// but this guard makes the script safe to run against a fresh container
	// that hasn't had the migration applied yet via docker-entrypoint.)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS auth_keys (
			id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			key_hash   VARCHAR(64) NOT NULL UNIQUE,
			tier       VARCHAR(20) NOT NULL DEFAULT 'free'
			            CHECK (tier IN ('free', 'premium')),
			label      VARCHAR(100),
			is_active  BOOLEAN     NOT NULL DEFAULT TRUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	if err != nil {
		log.Fatalf("FATAL: could not ensure auth_keys table: %v", err)
	}

	// ── 4. Generate, hash, insert ─────────────────────────────────────────
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║             GEO SAAS — API Key Seeder Results               ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	for _, spec := range keysToSeed {
		plainKey, keyHash, err := generateKey()
		if err != nil {
			log.Fatalf("FATAL: key generation failed: %v", err)
		}

		_, err = db.Exec(`
			INSERT INTO auth_keys (key_hash, tier, label)
			VALUES ($1, $2, $3)
			ON CONFLICT (key_hash) DO UPDATE
			    SET tier = EXCLUDED.tier,
			        label = EXCLUDED.label,
			        is_active = TRUE`,
			keyHash, spec.tier, spec.label,
		)
		if err != nil {
			log.Fatalf("FATAL: insert failed for %s: %v", spec.label, err)
		}

		fmt.Printf("  %-20s  tier=%-10s\n", spec.label, spec.tier)
		fmt.Printf("  Plain-text key : %s\n", plainKey)
		fmt.Printf("  SHA-256 hash   : %s\n", keyHash)
		fmt.Println()
	}

	fmt.Println("  ⚠  Store the plain-text keys securely — they are NOT saved")
	fmt.Println("     in the database and cannot be recovered after this run.")
	fmt.Println()
	fmt.Println("  Usage:")
	fmt.Println(`     curl -H "Authorization: Bearer <plain-text-key>" \`)
	fmt.Println(`          http://localhost:8080/api/v1/states`)
	fmt.Println()
}

// generateKey creates a random 32-byte key, hex-encodes it (64 chars plain text),
// and returns both the plain-text key and its SHA-256 hash (also hex-encoded).
func generateKey() (plainKey, hash string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("rand.Read: %w", err)
	}
	plainKey = hex.EncodeToString(raw) // 64-char hex string; "gsk_"-prefix optional
	sum := sha256.Sum256([]byte(plainKey))
	hash = fmt.Sprintf("%x", sum)
	return plainKey, hash, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
