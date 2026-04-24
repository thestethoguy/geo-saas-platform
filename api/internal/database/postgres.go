package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq" // postgres driver — blank import registers it
)

// NewPostgresDB opens a connection pool to PostgreSQL and verifies connectivity.
// The returned *sql.DB is safe for concurrent use and should be shared application-wide.
func NewPostgresDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open failed: %w", err)
	}

	// ── Connection pool tuning ──────────────────────────────────────────────
	// For a production API handling 1M+ req/day these defaults are conservative;
	// tune further based on Postgres max_connections setting.
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)

	// Verify the database is reachable before the server starts accepting traffic.
	if err = pingWithRetry(db, 5, 2*time.Second); err != nil {
		return nil, fmt.Errorf("postgres unreachable: %w", err)
	}

	log.Println("[db] PostgreSQL connection pool established ✓")
	return db, nil
}

// pingWithRetry attempts to ping Postgres up to `attempts` times with a delay
// between retries — useful during docker-compose startup when Postgres needs a
// few seconds to become ready.
func pingWithRetry(db *sql.DB, attempts int, delay time.Duration) error {
	var err error
	for i := 1; i <= attempts; i++ {
		if err = db.Ping(); err == nil {
			return nil
		}
		log.Printf("[db] Postgres not ready (attempt %d/%d): %v — retrying in %s", i, attempts, err, delay)
		time.Sleep(delay)
	}
	return err
}
