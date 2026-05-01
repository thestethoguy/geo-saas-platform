// Geo SaaS — Bulk Ingestion Engine (V1 Production)
//
// Full-reload ETL for ~650k villages from master_india_villages.csv.
// Performance strategy:
//   - Hierarchy tables (states/districts/sub_districts): batch INSERT ON CONFLICT DO NOTHING
//   - Villages: PostgreSQL COPY protocol via pq.CopyIn in chunks of 2,000 (fastest bulk loader)
//   - Typesense:  JSONL import in batches of 1,000
//
// ⚠ This script TRUNCATES the villages table and drops+recreates the Typesense
//   collection before loading. It is a clean-slate production loader.
//
// Usage:
//
//	go run main.go
//	CSV_PATH=/path/to/custom.csv go run main.go
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/lib/pq"
)

// ─────────────────────────────────────────────────────────────────────────────
// Constants
// ─────────────────────────────────────────────────────────────────────────────

const (
	villageCopyChunk = 2_000 // villages per COPY transaction
	tsBatchSize      = 1_000 // Typesense JSONL batch size
	logEvery         = 1_000 // progress log interval (villages)
	tsCollection     = "villages"
)

// ─────────────────────────────────────────────────────────────────────────────
// Data structures
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// Internal hierarchy row types (used only inside ingestHierarchy helpers)
// ─────────────────────────────────────────────────────────────────────────────

type stateRow    struct{ lgd, name string }
type distRow     struct{ lgd, name, stateLGD string }
type subDistRow  struct{ lgd, name, districtLGD string }

// CSVRow is one row from master_india_villages.csv.
type CSVRow struct {
	StateLGD        string
	StateName       string
	DistrictLGD     string
	DistrictName    string
	SubDistrictLGD  string
	SubDistrictName string
	VillageLGD      string
	VillageName     string
}

// VillageDoc is the flattened Typesense document (V1 schema, 6 fields).
type VillageDoc struct {
	ID              string `json:"id"`
	VillageName     string `json:"village_name"`
	SubDistrictName string `json:"sub_district_name"`
	DistrictName    string `json:"district_name"`
	StateName       string `json:"state_name"`
	LGDCode         string `json:"lgd_code"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Config
// ─────────────────────────────────────────────────────────────────────────────

type config struct {
	postgresDSN       string
	typesenseBase     string // full URL e.g. "http://localhost:8108"
	typesenseAPIKey   string
	csvPath           string
}

func loadConfig() config {
	// Try cwd/.env first  → works when run from project root: go run scripts/ingest/main.go
	// Fall back to ../../.env → works when run from scripts/ingest/: go run main.go
	if err := godotenv.Load(); err != nil {
		if fbErr := godotenv.Load("../../.env"); fbErr != nil {
			log.Printf("WARN: .env not found in cwd or ../../.env — relying on shell environment")
		} else {
			log.Println(".env loaded from ../../.env")
		}
	} else {
		log.Println(".env loaded from cwd/.env")
	}

	// ── PostgreSQL DSN ─────────────────────────────────────────────────────
	// Priority: DATABASE_URL (single connection string) → individual vars.
	// DATABASE_URL is the standard set by Render, Railway, Heroku, etc.
	var dsn string
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		log.Println("[config] PostgreSQL: using DATABASE_URL")
		dsn = dbURL
	} else {
		host     := getEnv("POSTGRES_HOST",     "")
		port     := getEnv("POSTGRES_PORT",     "5432")
		user     := getEnv("POSTGRES_USER",     "")
		password := getEnv("POSTGRES_PASSWORD", "")
		dbname   := getEnv("POSTGRES_DB",       "")

		if host == "" || user == "" || dbname == "" {
			log.Fatalf("FATAL: DATABASE_URL is missing. Check your .env file path.\n" +
				"  Tried: .env (cwd) and ../../.env (project root fallback)\n" +
				"  Set DATABASE_URL or POSTGRES_HOST/USER/PASSWORD/DB in your environment.")
		}
		dsn = fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			host, port, user, password, dbname,
		)
	}

	protocol := getEnv("TYPESENSE_PROTOCOL", "http")
	tsHost   := getEnv("TYPESENSE_HOST", "localhost")
	tsPort   := getEnv("TYPESENSE_PORT", "8108")

	// Omit the port from the base URL when it is the default for the protocol
	// (https+443 or http+80). Render's edge proxy rejects an explicit :443 in
	// the Host header, causing a TLS timeout even though the server is up.
	var tsBase string
	if (protocol == "https" && tsPort == "443") || (protocol == "http" && tsPort == "80") {
		tsBase = fmt.Sprintf("%s://%s", protocol, tsHost)
	} else {
		tsBase = fmt.Sprintf("%s://%s:%s", protocol, tsHost, tsPort)
	}

	return config{
		postgresDSN:     dsn,
		typesenseBase:   tsBase,
		typesenseAPIKey: getEnv("TYPESENSE_API_KEY", "typesense-dev-key"),
		// Plain relative path — resolves from wherever the script is invoked.
		// Override with: CSV_PATH=/absolute/path/to/file.csv go run scripts/ingest/main.go
		csvPath: getEnv("CSV_PATH", "data/processed/master_india_villages.csv"),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// ─────────────────────────────────────────────────────────────────────────────
// Typesense document selection: priority-first, hard-capped at tsIndexLimit
// ─────────────────────────────────────────────────────────────────────────────

// tsIndexLimit is the maximum number of documents pushed to Typesense.
// Kept at 15,000 so the index fits comfortably within Render's free-tier
// 512 MB RAM ceiling (~20 MB for 15k documents leaves ample headroom).
const tsIndexLimit = 15_000

// priorityKeywords contains lowercase substrings matched against village_name.
// Any document whose village_name contains one of these keywords is guaranteed
// a slot in the first part of the Typesense batch before fill records are added.
var priorityKeywords = []string{
	// ── Pune / Maharashtra IT corridors ──────────────────────────────────────
	"hinjavadi", // Hinjewadi Phase I/II/III IT park (census: Hinjavadi)
	"wakad",
	"baner",
	"kharadi",
	"hadapsar",
	"kothrud",
	"viman nagar",
	"magarpatta",
	"pimpri",
	"chinchwad",
	// ── Bengaluru / Karnataka ─────────────────────────────────────────────────
	"koramangala",
	"yelahanka",
	"yalahanka", // census spelling of Yelahanka
	"whitefield",
	"marathahalli",
	"indiranagar",
	"sarjapur",
	"electronic city",
	"bellandur",
	"hebbal",
	// ── Mumbai / Thane / Navi Mumbai ─────────────────────────────────────────
	"andheri",
	"bandra",
	"powai",
	"kurla",
	"thane",
	"vashi",
	"belapur",
	// ── Hyderabad / Telangana-adjacent Andhra Pradesh ─────────────────────────
	"hitech city",
	"madhapur",
	"gachibowli",
	"kondapur",
	"kukatpally",
	"miyapur",
	// ── Chennai / Tamil Nadu ──────────────────────────────────────────────────
	"sholinganallur",
	"perungudi",
	"ambattur",
	"porur",
	// ── Delhi / NCR ───────────────────────────────────────────────────────────
	"dwarka",
	"rohini",
	"gurugram",
	"noida",
	"gurgaon",
	// ── Other well-known cities represented in the dataset ────────────────────
	"bangalore",
	"bengaluru",
	"mumbai",
	"delhi",
	"kolkata",
	"chennai",
	"jaipur",
	"lucknow",
	"patna",
	"bhubaneswar",
	"indore",
	"bhopal",
	"surat",
	"ahmedabad",
	"vadodara",
	"chandigarh",
}

// prioritizeAndLimit selects up to `limit` VillageDocs for Typesense indexing.
//
// Selection order:
//  1. Priority docs — any village whose name (lowercase) contains one of the
//     priorityKeywords. These are guaranteed to be included first.
//  2. Fill docs    — the remaining villages in CSV order, appended until the
//     total reaches `limit`.
//
// This ensures demo-critical locations are always present regardless of which
// slice of the 564k corpus the fill picks up.
func prioritizeAndLimit(docs []VillageDoc, limit int) []VillageDoc {
	if len(docs) <= limit {
		return docs // nothing to trim
	}

	priority := make([]VillageDoc, 0, 512)
	fill := make([]VillageDoc, 0, limit)

	prioritySeen := make(map[string]bool, 512)

	for _, d := range docs {
		lower := strings.ToLower(d.VillageName)
		matched := false
		for _, kw := range priorityKeywords {
			if strings.Contains(lower, kw) {
				matched = true
				break
			}
		}
		if matched && !prioritySeen[d.ID] {
			prioritySeen[d.ID] = true
			priority = append(priority, d)
		} else if !matched {
			fill = append(fill, d)
		}
	}

	// Cap priority slice itself if it somehow exceeds the limit
	if len(priority) > limit {
		priority = priority[:limit]
	}

	// Fill remaining slots
	remaining := limit - len(priority)
	if remaining > 0 && len(fill) > 0 {
		if remaining < len(fill) {
			fill = fill[:remaining]
		}
		priority = append(priority, fill...)
	}

	log.Printf("[limit] Typesense batch: %d priority + %d fill = %d / %d total docs",
		len(priority)-len(fill), len(fill), len(priority), len(docs))

	return priority
}

// ─────────────────────────────────────────────────────────────────────────────
// Main
// ─────────────────────────────────────────────────────────────────────────────

func main() {
	log.SetFlags(log.Ltime | log.Lmsgprefix)
	log.SetPrefix("[ingest] ")
	start := time.Now()

	cfg := loadConfig()

	// 1. Connect Postgres
	db, err := connectPostgres(cfg.postgresDSN)
	if err != nil {
		log.Fatalf("FATAL: %v", err)
	}
	defer db.Close()

	// 2. Read CSV
	rows, err := readCSV(cfg.csvPath)
	if err != nil {
		log.Fatalf("FATAL: CSV read failed (%s): %v", cfg.csvPath, err)
	}
	log.Printf("CSV loaded — %d rows from %s", len(rows), cfg.csvPath)

	// 3. Ingest hierarchy (states → districts → sub_districts)
	subDistMap, err := ingestHierarchy(db, rows)
	if err != nil {
		log.Fatalf("FATAL: hierarchy ingest failed: %v", err)
	}

	// 4. Bulk-load villages into Postgres (TRUNCATE + COPY — full 564k dataset)
	docs, err := ingestVillages(db, rows, subDistMap)
	if err != nil {
		log.Fatalf("FATAL: village ingest failed: %v", err)
	}

	// 4b. Cap the Typesense batch to tsIndexLimit to avoid OOM on the free-tier
	//     512 MB Render instance. Priority villages (Hinjewadi, Koramangala,
	//     Yelahanka, etc.) are guaranteed slots before fill records are added.
	tsDocs := prioritizeAndLimit(docs, tsIndexLimit)
	log.Printf("[limit] Typesense will index %d of %d total villages (cap=%d)",
		len(tsDocs), len(docs), tsIndexLimit)

	// 5. Typesense — ping, recreate collection, bulk index
	if err := pingTypesense(cfg.typesenseBase, cfg.typesenseAPIKey); err != nil {
		log.Fatalf("FATAL: Typesense unreachable at %s: %v", cfg.typesenseBase, err)
	}
	if err := recreateTypesenseCollection(cfg.typesenseBase, cfg.typesenseAPIKey); err != nil {
		log.Fatalf("FATAL: Typesense collection setup failed: %v", err)
	}
	if err := indexTypesense(cfg.typesenseBase, cfg.typesenseAPIKey, tsDocs); err != nil {
		log.Fatalf("FATAL: Typesense indexing failed: %v", err)
	}

	log.Printf("✅ Ingestion complete in %s", time.Since(start).Round(time.Second))
}

// ─────────────────────────────────────────────────────────────────────────────
// Postgres: connect
// ─────────────────────────────────────────────────────────────────────────────

func connectPostgres(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	for i := 1; i <= 5; i++ {
		if err = db.Ping(); err == nil {
			log.Println("PostgreSQL connected ✓")
			return db, nil
		}
		log.Printf("Postgres not ready (%d/5): %v — retrying in 3s", i, err)
		time.Sleep(3 * time.Second)
	}
	return nil, fmt.Errorf("postgres unreachable after 5 attempts: %w", err)
}

// ─────────────────────────────────────────────────────────────────────────────
// Postgres: hierarchy ingest
// ─────────────────────────────────────────────────────────────────────────────

// ingestHierarchy inserts all unique states, districts and sub_districts in
// three small batch INSERTs, then returns an in-memory map of
// sub_district lgd_code → UUID string for use during village loading.
func ingestHierarchy(db *sql.DB, rows []CSVRow) (map[string]string, error) {
	ctx := context.Background()

	// ── Collect unique entries (preserve first-seen order) ────────────────
	var (
		stateSeen   = map[string]bool{}
		distSeen    = map[string]bool{}
		subDistSeen = map[string]bool{}

		states       []stateRow
		districts    []distRow
		subDistricts []subDistRow
	)

	for _, r := range rows {
		if !stateSeen[r.StateLGD] {
			stateSeen[r.StateLGD] = true
			states = append(states, stateRow{r.StateLGD, r.StateName})
		}
		if !distSeen[r.DistrictLGD] {
			distSeen[r.DistrictLGD] = true
			districts = append(districts, distRow{r.DistrictLGD, r.DistrictName, r.StateLGD})
		}
		if !subDistSeen[r.SubDistrictLGD] {
			subDistSeen[r.SubDistrictLGD] = true
			subDistricts = append(subDistricts, subDistRow{r.SubDistrictLGD, r.SubDistrictName, r.DistrictLGD})
		}
	}

	log.Printf("Hierarchy: %d states, %d districts, %d sub-districts to insert",
		len(states), len(districts), len(subDistricts))

	// ── 1. Insert states ──────────────────────────────────────────────────
	if err := batchInsertStates(ctx, db, states); err != nil {
		return nil, fmt.Errorf("insert states: %w", err)
	}
	stateUUIDs, err := loadUUIDs(ctx, db, "states")
	if err != nil {
		return nil, fmt.Errorf("load state UUIDs: %w", err)
	}
	log.Printf("States done ✓  (%d UUIDs loaded)", len(stateUUIDs))

	// ── 2. Insert districts ───────────────────────────────────────────────
	if err := batchInsertDistricts(ctx, db, districts, stateUUIDs); err != nil {
		return nil, fmt.Errorf("insert districts: %w", err)
	}
	districtUUIDs, err := loadUUIDs(ctx, db, "districts")
	if err != nil {
		return nil, fmt.Errorf("load district UUIDs: %w", err)
	}
	log.Printf("Districts done ✓  (%d UUIDs loaded)", len(districtUUIDs))

	// ── 3. Insert sub_districts ───────────────────────────────────────────
	if err := batchInsertSubDistricts(ctx, db, subDistricts, districtUUIDs); err != nil {
		return nil, fmt.Errorf("insert sub_districts: %w", err)
	}
	subDistUUIDs, err := loadUUIDs(ctx, db, "sub_districts")
	if err != nil {
		return nil, fmt.Errorf("load sub_district UUIDs: %w", err)
	}
	log.Printf("Sub-districts done ✓  (%d UUIDs loaded)", len(subDistUUIDs))

	return subDistUUIDs, nil
}

// batchInsertStates inserts all states in a single multi-row INSERT.
func batchInsertStates(ctx context.Context, db *sql.DB, states []stateRow) error {
	if len(states) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("INSERT INTO states (lgd_code, name) VALUES ")
	args := make([]interface{}, 0, len(states)*2)
	for i, s := range states {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "($%d,$%d)", i*2+1, i*2+2)
		args = append(args, s.lgd, s.name)
	}
	sb.WriteString(" ON CONFLICT (lgd_code) DO NOTHING")
	_, err := db.ExecContext(ctx, sb.String(), args...)
	return err
}

// batchInsertDistricts inserts all districts resolving state UUIDs from the map.
func batchInsertDistricts(ctx context.Context, db *sql.DB, districts []distRow, stateUUIDs map[string]string) error {
	if len(districts) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("INSERT INTO districts (lgd_code, name, state_id) VALUES ")
	args := make([]interface{}, 0, len(districts)*3)
	for i, d := range districts {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "($%d,$%d,$%d::uuid)", i*3+1, i*3+2, i*3+3)
		args = append(args, d.lgd, d.name, stateUUIDs[d.stateLGD])
	}
	sb.WriteString(" ON CONFLICT (lgd_code) DO NOTHING")
	_, err := db.ExecContext(ctx, sb.String(), args...)
	return err
}

// batchInsertSubDistricts inserts all sub_districts resolving district UUIDs.
func batchInsertSubDistricts(ctx context.Context, db *sql.DB, subs []subDistRow, districtUUIDs map[string]string) error {
	if len(subs) == 0 {
		return nil
	}
	// Sub-districts can exceed 6000 rows × 3 params = 18k params — well within the 65535 limit.
	var sb strings.Builder
	sb.WriteString("INSERT INTO sub_districts (lgd_code, name, district_id) VALUES ")
	args := make([]interface{}, 0, len(subs)*3)
	for i, s := range subs {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "($%d,$%d,$%d::uuid)", i*3+1, i*3+2, i*3+3)
		args = append(args, s.lgd, s.name, districtUUIDs[s.districtLGD])
	}
	sb.WriteString(" ON CONFLICT (lgd_code) DO NOTHING")
	_, err := db.ExecContext(ctx, sb.String(), args...)
	return err
}

// loadUUIDs returns lgd_code → id::text for any geo table.
func loadUUIDs(ctx context.Context, db *sql.DB, table string) (map[string]string, error) {
	//nolint:gosec — table name is an internal constant, not user input
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT lgd_code, id::text FROM %s", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var lgd, id string
		if err := rows.Scan(&lgd, &id); err != nil {
			return nil, err
		}
		m[lgd] = id
	}
	return m, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// Postgres: village bulk load via COPY
// ─────────────────────────────────────────────────────────────────────────────

// ingestVillages truncates the villages table, then loads all rows using
// PostgreSQL's COPY protocol in chunks of villageCopyChunk for progress logging.
// It simultaneously builds the []VillageDoc slice for Typesense indexing.
func ingestVillages(db *sql.DB, rows []CSVRow, subDistMap map[string]string) ([]VillageDoc, error) {
	ctx := context.Background()

	// De-duplicate villages by lgd_code (CSV may have duplicates)
	seen := make(map[string]bool, len(rows))
	unique := rows[:0]
	for _, r := range rows {
		if r.VillageLGD == "" || seen[r.VillageLGD] {
			continue
		}
		seen[r.VillageLGD] = true
		unique = append(unique, r)
	}
	total := len(unique)
	log.Printf("Villages to load: %d unique (of %d CSV rows)", total, len(rows))

	// TRUNCATE to ensure a clean slate before COPY
	if _, err := db.ExecContext(ctx, "TRUNCATE villages"); err != nil {
		return nil, fmt.Errorf("TRUNCATE villages: %w", err)
	}
	log.Println("Villages table truncated ✓")

	docs := make([]VillageDoc, 0, total)
	loaded := 0
	skipped := 0

	for chunkStart := 0; chunkStart < total; chunkStart += villageCopyChunk {
		end := chunkStart + villageCopyChunk
		if end > total {
			end = total
		}
		chunk := unique[chunkStart:end]

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}

		stmt, err := tx.Prepare(pq.CopyIn("villages", "lgd_code", "name", "sub_district_id"))
		if err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("prepare COPY: %w", err)
		}

		for _, r := range chunk {
			subDistID, ok := subDistMap[r.SubDistrictLGD]
			if !ok {
				log.Printf("WARN: sub_district lgd_code %q not found — skipping village %q",
					r.SubDistrictLGD, r.VillageName)
				skipped++
				continue
			}
			if _, err := stmt.Exec(r.VillageLGD, r.VillageName, subDistID); err != nil {
				_ = tx.Rollback()
				return nil, fmt.Errorf("COPY row (lgd=%s): %w", r.VillageLGD, err)
			}
			docs = append(docs, VillageDoc{
				ID:              r.VillageLGD,
				VillageName:     r.VillageName,
				SubDistrictName: r.SubDistrictName,
				DistrictName:    r.DistrictName,
				StateName:       r.StateName,
				LGDCode:         r.VillageLGD,
			})
		}

		// Flush buffer to Postgres
		if _, err := stmt.Exec(); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("COPY flush: %w", err)
		}
		if err := stmt.Close(); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("COPY close: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit: %w", err)
		}

		loaded += len(chunk)
		log.Printf("[Ingest] Successfully processed %d / %d villages...", loaded, total)
	}

	log.Printf("Villages loaded ✓  (%d inserted, %d skipped)", loaded-skipped, skipped)
	return docs, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Typesense helpers
// ─────────────────────────────────────────────────────────────────────────────

var typesenseCollectionSchema = map[string]interface{}{
	"name": tsCollection,
	"fields": []map[string]interface{}{
		{"name": "id",                "type": "string"},
		{"name": "village_name",      "type": "string"},
		{"name": "sub_district_name", "type": "string", "facet": true},
		{"name": "district_name",     "type": "string", "facet": true},
		{"name": "state_name",        "type": "string", "facet": true},
		{"name": "lgd_code",          "type": "string"},
	},
}

// pingTypesense confirms Typesense is reachable and the API key is valid.
// Uses GET /collections (works on both self-hosted and managed instances).
func pingTypesense(base, apiKey string) error {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, base+"/collections", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-TYPESENSE-API-KEY", apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid API key (401)")
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("server error: HTTP %d", resp.StatusCode)
	}
	log.Printf("Typesense connected ✓  (%s)", base)
	return nil
}

// recreateTypesenseCollection drops the collection if it exists, then creates
// it fresh with the V1 schema. Ensures the schema is always in sync.
func recreateTypesenseCollection(base, apiKey string) error {
	client := &http.Client{Timeout: 15 * time.Second}

	// Drop if exists
	req, _ := http.NewRequest(http.MethodDelete, base+"/collections/"+tsCollection, nil)
	req.Header.Set("X-TYPESENSE-API-KEY", apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("delete collection: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		log.Printf("Typesense collection %q dropped (or did not exist) ✓", tsCollection)
	} else {
		return fmt.Errorf("delete collection returned HTTP %d", resp.StatusCode)
	}

	// Create
	body, _ := json.Marshal(typesenseCollectionSchema)
	req2, _ := http.NewRequest(http.MethodPost, base+"/collections", bytes.NewReader(body))
	req2.Header.Set("X-TYPESENSE-API-KEY", apiKey)
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusCreated {
		return fmt.Errorf("create collection returned HTTP %d", resp2.StatusCode)
	}
	log.Printf("Typesense collection %q created ✓", tsCollection)
	return nil
}

// indexTypesense sends all VillageDoc records to Typesense in JSONL batches.
func indexTypesense(base, apiKey string, docs []VillageDoc) error {
	if len(docs) == 0 {
		log.Println("No Typesense documents to index.")
		return nil
	}

	client := &http.Client{Timeout: 60 * time.Second}
	url := fmt.Sprintf("%s/collections/%s/documents/import?action=upsert", base, tsCollection)
	total := len(docs)
	indexed := 0

	for batchStart := 0; batchStart < total; batchStart += tsBatchSize {
		end := batchStart + tsBatchSize
		if end > total {
			end = total
		}
		batch := docs[batchStart:end]

		// Encode as JSONL (newline-delimited JSON)
		var buf bytes.Buffer
		for _, d := range batch {
			line, err := json.Marshal(d)
			if err != nil {
				return fmt.Errorf("marshal doc %s: %w", d.ID, err)
			}
			buf.Write(line)
			buf.WriteByte('\n')
		}

		req, err := http.NewRequest(http.MethodPost, url, &buf)
		if err != nil {
			return err
		}
		req.Header.Set("X-TYPESENSE-API-KEY", apiKey)
		req.Header.Set("Content-Type", "text/plain")

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("Typesense import batch starting at %d: %w", batchStart, err)
		}

		// Parse JSONL response — each line is {"success":true} or an error
		batchErr := 0
		scanner := newLineScanner(resp.Body)
		for scanner.Scan() {
			var result map[string]interface{}
			if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
				continue
			}
			if success, ok := result["success"].(bool); !ok || !success {
				batchErr++
			}
		}
		resp.Body.Close()

		indexed += len(batch)
		if batchErr > 0 {
			log.Printf("[Ingest] Typesense batch %d–%d: %d errors", batchStart+1, end, batchErr)
		} else {
			log.Printf("[Ingest] Typesense: indexed %d / %d documents ✓", indexed, total)
		}
	}

	log.Printf("Typesense indexing complete ✓  (%d documents)", indexed)
	return nil
}

// newLineScanner wraps an io.ReadCloser in a line-by-line bytes.Scanner.
func newLineScanner(r io.ReadCloser) *lineScanner {
	data, _ := io.ReadAll(r)
	return &lineScanner{lines: bytes.Split(data, []byte("\n")), pos: 0}
}

type lineScanner struct {
	lines [][]byte
	pos   int
	cur   []byte
}

func (s *lineScanner) Scan() bool {
	for s.pos < len(s.lines) {
		s.cur = bytes.TrimSpace(s.lines[s.pos])
		s.pos++
		if len(s.cur) > 0 {
			return true
		}
	}
	return false
}

func (s *lineScanner) Bytes() []byte { return s.cur }

// ─────────────────────────────────────────────────────────────────────────────
// CSV reader
// ─────────────────────────────────────────────────────────────────────────────

func readCSV(path string) ([]CSVRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true // tolerate imperfect quoting in census files

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		colIdx[strings.TrimSpace(strings.ToLower(h))] = i
	}

	required := []string{
		"state_lgd_code", "state_name",
		"district_lgd_code", "district_name",
		"sub_district_lgd_code", "sub_district_name",
		"village_lgd_code", "village_name",
	}
	for _, col := range required {
		if _, ok := colIdx[col]; !ok {
			return nil, fmt.Errorf("missing required column %q (found: %v)", col, header)
		}
	}

	get := func(record []string, col string) string {
		i, ok := colIdx[col]
		if !ok || i >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[i])
	}

	rows := make([]CSVRow, 0, 700_000)
	lineNum := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
		lineNum++
		rows = append(rows, CSVRow{
			StateLGD:        get(record, "state_lgd_code"),
			StateName:       get(record, "state_name"),
			DistrictLGD:     get(record, "district_lgd_code"),
			DistrictName:    get(record, "district_name"),
			SubDistrictLGD:  get(record, "sub_district_lgd_code"),
			SubDistrictName: get(record, "sub_district_name"),
			VillageLGD:      get(record, "village_lgd_code"),
			VillageName:     get(record, "village_name"),
		})
	}
	return rows, nil
}
