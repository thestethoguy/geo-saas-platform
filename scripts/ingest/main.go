// Package main is a standalone ETL script that:
//  1. Reads LGD geographical data from a CSV file
//  2. Inserts it hierarchically into PostgreSQL (states → districts → sub_districts → villages)
//  3. Indexes every village as a full-address search document in Typesense
//
// Usage:
//
//	go run main.go                          # uses default paths
//	CSV_PATH=../../data/custom.csv go run main.go
package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

// ─────────────────────────────────────────────────────────────────────────────
// Data structures
// ─────────────────────────────────────────────────────────────────────────────

// CSVRow mirrors one line in sample_lgd_data.csv
type CSVRow struct {
	StateLGD       string
	StateName      string
	DistrictLGD    string
	DistrictName   string
	SubDistrictLGD string
	SubDistrictName string
	VillageLGD     string
	VillageName    string
	CensusCode     string
	Pincode        string
	Latitude       string
	Longitude      string
	Population     string
}

// VillageDoc is the flattened document we index in Typesense
type VillageDoc struct {
	ID              string  `json:"id"`              // village lgd_code (must be string in TS)
	VillageName     string  `json:"village_name"`
	SubDistrictName string  `json:"sub_district_name"`
	DistrictName    string  `json:"district_name"`
	StateName       string  `json:"state_name"`
	FullAddress     string  `json:"full_address"`    // "Village, Sub-District, District, State, India"
	VillageLGD      string  `json:"village_lgd"`
	Pincode         string  `json:"pincode"`
	Latitude        float64 `json:"latitude"`
	Longitude       float64 `json:"longitude"`
	Population      int64   `json:"population"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Config
// ─────────────────────────────────────────────────────────────────────────────

type config struct {
	postgresDSN     string
	typesenseHost   string
	typesensePort   string
	typesenseAPIKey string
	csvPath         string
}

func loadConfig() config {
	// Walk up until we find the .env file (project root)
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	_ = godotenv.Load(filepath.Join(projectRoot, ".env"))

	host     := getEnv("POSTGRES_HOST", "localhost")
	port     := getEnv("POSTGRES_PORT", "5432")
	user     := getEnv("POSTGRES_USER", "geouser")
	password := getEnv("POSTGRES_PASSWORD", "geopassword")
	dbname   := getEnv("POSTGRES_DB", "geosaas")

	csvDefault := filepath.Join(projectRoot, "data", "sample_lgd_data.csv")

	return config{
		postgresDSN: fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			host, port, user, password, dbname,
		),
		typesenseHost:   getEnv("TYPESENSE_HOST", "localhost"),
		typesensePort:   getEnv("TYPESENSE_PORT", "8108"),
		typesenseAPIKey: getEnv("TYPESENSE_API_KEY", "typesense-dev-key"),
		csvPath:         getEnv("CSV_PATH", csvDefault),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// ─────────────────────────────────────────────────────────────────────────────
// Main
// ─────────────────────────────────────────────────────────────────────────────

func main() {
	log.SetFlags(log.Ltime | log.Lmsgprefix)
	log.SetPrefix("[ingest] ")

	cfg := loadConfig()

	// ── 1. Connect to Postgres ────────────────────────────────────────────
	db, err := connectPostgres(cfg.postgresDSN)
	if err != nil {
		log.Fatalf("FATAL: Postgres connection failed: %v", err)
	}
	defer db.Close()

	// ── 2. Read CSV ───────────────────────────────────────────────────────
	rows, err := readCSV(cfg.csvPath)
	if err != nil {
		log.Fatalf("FATAL: Could not read CSV at %s: %v", cfg.csvPath, err)
	}
	log.Printf("CSV loaded — %d data rows found in %s", len(rows), cfg.csvPath)

	// ── 3. Ingest into Postgres ───────────────────────────────────────────
	docs, err := ingestPostgres(db, rows)
	if err != nil {
		log.Fatalf("FATAL: Postgres ingestion failed: %v", err)
	}

	// ── 4. Ping Typesense (fail fast if unreachable) ─────────────────────
	tsBase := fmt.Sprintf("http://%s:%s", cfg.typesenseHost, cfg.typesensePort)
	if err := pingTypesense(tsBase, cfg.typesenseAPIKey); err != nil {
		log.Fatalf("FATAL: Typesense unreachable at %s: %v", tsBase, err)
	}

	// ── 5. Ensure Typesense collection exists ─────────────────────────────
	if err := ensureTypesenseCollection(tsBase, cfg.typesenseAPIKey); err != nil {
		log.Fatalf("FATAL: Typesense collection setup failed: %v", err)
	}

	// ── 6. Index villages in Typesense ────────────────────────────────────
	if err := indexTypesense(tsBase, cfg.typesenseAPIKey, docs); err != nil {
		log.Fatalf("FATAL: Typesense indexing failed: %v", err)
	}

	log.Println("✅ Ingestion complete!")
}

// ─────────────────────────────────────────────────────────────────────────────
// PostgreSQL helpers
// ─────────────────────────────────────────────────────────────────────────────

func connectPostgres(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	// Retry ping — Postgres may still be starting when this script runs
	for i := 1; i <= 5; i++ {
		if err = db.Ping(); err == nil {
			log.Println("PostgreSQL connected ✓")
			return db, nil
		}
		log.Printf("Postgres not ready (attempt %d/5): %v — retrying in 3s", i, err)
		time.Sleep(3 * time.Second)
	}
	return nil, fmt.Errorf("postgres unreachable after 5 attempts: %w", err)
}

// ingestPostgres walks the deduplicated hierarchy and upserts rows level by level.
// It returns a slice of VillageDoc ready for Typesense indexing.
func ingestPostgres(db *sql.DB, rows []CSVRow) ([]VillageDoc, error) {
	ctx := context.Background()

	// Deduplication maps: lgd_code → internal DB id
	stateIDs       := make(map[string]int)
	districtIDs    := make(map[string]int)
	subDistrictIDs := make(map[string]int)

	// Counters
	var (
		statesInserted, statesSkipped           int
		districtsInserted, districtsSkipped     int
		subDistInserted, subDistSkipped         int
		villagesInserted, villagesSkipped       int
	)

	var docs []VillageDoc

	// ── BEGIN transaction ──────────────────────────────────────────────────
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, row := range rows {

		// ── State ──────────────────────────────────────────────────────────
		if _, seen := stateIDs[row.StateLGD]; !seen {
			id, inserted, e := upsertState(ctx, tx, row.StateLGD, row.StateName)
			if e != nil {
				return nil, fmt.Errorf("upsert state %s: %w", row.StateName, e)
			}
			stateIDs[row.StateLGD] = id
			if inserted {
				log.Printf("  ✚ Inserted State    : %s (lgd=%s)", row.StateName, row.StateLGD)
				statesInserted++
			} else {
				statesSkipped++
			}
		}

		// ── District ───────────────────────────────────────────────────────
		if _, seen := districtIDs[row.DistrictLGD]; !seen {
			stateID := stateIDs[row.StateLGD]
			id, inserted, e := upsertDistrict(ctx, tx, row.DistrictLGD, row.DistrictName, stateID)
			if e != nil {
				return nil, fmt.Errorf("upsert district %s: %w", row.DistrictName, e)
			}
			districtIDs[row.DistrictLGD] = id
			if inserted {
				log.Printf("  ✚ Inserted District : %s (lgd=%s)", row.DistrictName, row.DistrictLGD)
				districtsInserted++
			} else {
				districtsSkipped++
			}
		}

		// ── Sub-District ───────────────────────────────────────────────────
		if _, seen := subDistrictIDs[row.SubDistrictLGD]; !seen {
			districtID := districtIDs[row.DistrictLGD]
			id, inserted, e := upsertSubDistrict(ctx, tx, row.SubDistrictLGD, row.SubDistrictName, districtID)
			if e != nil {
				return nil, fmt.Errorf("upsert sub_district %s: %w", row.SubDistrictName, e)
			}
			subDistrictIDs[row.SubDistrictLGD] = id
			if inserted {
				log.Printf("  ✚ Inserted Sub-Dist : %s (lgd=%s)", row.SubDistrictName, row.SubDistrictLGD)
				subDistInserted++
			} else {
				subDistSkipped++
			}
		}

		// ── Village ────────────────────────────────────────────────────────
		subDistID := subDistrictIDs[row.SubDistrictLGD]
		inserted, e := upsertVillage(ctx, tx, row, subDistID)
		if e != nil {
			return nil, fmt.Errorf("upsert village %s: %w", row.VillageName, e)
		}
		if inserted {
			log.Printf("  ✚ Inserted Village  : %s (lgd=%s)", row.VillageName, row.VillageLGD)
			villagesInserted++
		} else {
			villagesSkipped++
		}

		// Build Typesense document (regardless of insert/skip so we always index)
		docs = append(docs, buildDoc(row))
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	log.Printf("\n── Postgres Summary ─────────────────────────────────")
	log.Printf("   States       : %d inserted, %d skipped (already existed)", statesInserted, statesSkipped)
	log.Printf("   Districts    : %d inserted, %d skipped", districtsInserted, districtsSkipped)
	log.Printf("   Sub-Districts: %d inserted, %d skipped", subDistInserted, subDistSkipped)
	log.Printf("   Villages     : %d inserted, %d skipped", villagesInserted, villagesSkipped)
	log.Println("─────────────────────────────────────────────────────")

	return docs, nil
}

// upsertState inserts a state if the lgd_code doesn't exist.
// Returns the internal id and whether a new row was created.
func upsertState(ctx context.Context, tx *sql.Tx, lgd, name string) (int, bool, error) {
	var id int
	var isNew bool
	err := tx.QueryRowContext(ctx, `
		INSERT INTO states (lgd_code, name)
		VALUES ($1, $2)
		ON CONFLICT (lgd_code) DO NOTHING
		RETURNING id, true
	`, lgd, name).Scan(&id, &isNew)

	if err == sql.ErrNoRows {
		// Row already existed — fetch its id
		err = tx.QueryRowContext(ctx, `SELECT id FROM states WHERE lgd_code = $1`, lgd).Scan(&id)
		return id, false, err
	}
	return id, isNew, err
}

func upsertDistrict(ctx context.Context, tx *sql.Tx, lgd, name string, stateID int) (int, bool, error) {
	var id int
	var isNew bool
	err := tx.QueryRowContext(ctx, `
		INSERT INTO districts (lgd_code, name, state_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (lgd_code) DO NOTHING
		RETURNING id, true
	`, lgd, name, stateID).Scan(&id, &isNew)

	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx, `SELECT id FROM districts WHERE lgd_code = $1`, lgd).Scan(&id)
		return id, false, err
	}
	return id, isNew, err
}

func upsertSubDistrict(ctx context.Context, tx *sql.Tx, lgd, name string, districtID int) (int, bool, error) {
	var id int
	var isNew bool
	err := tx.QueryRowContext(ctx, `
		INSERT INTO sub_districts (lgd_code, name, district_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (lgd_code) DO NOTHING
		RETURNING id, true
	`, lgd, name, districtID).Scan(&id, &isNew)

	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx, `SELECT id FROM sub_districts WHERE lgd_code = $1`, lgd).Scan(&id)
		return id, false, err
	}
	return id, isNew, err
}

func upsertVillage(ctx context.Context, tx *sql.Tx, row CSVRow, subDistID int) (bool, error) {
	var isNew bool
	var id int

	lat, _ := strconv.ParseFloat(strings.TrimSpace(row.Latitude), 64)
	lon, _ := strconv.ParseFloat(strings.TrimSpace(row.Longitude), 64)
	pop, _ := strconv.ParseInt(strings.TrimSpace(row.Population), 10, 64)

	err := tx.QueryRowContext(ctx, `
		INSERT INTO villages (lgd_code, name, sub_district_id, census_code, pincode, latitude, longitude, population)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (lgd_code) DO NOTHING
		RETURNING id, true
	`, row.VillageLGD, row.VillageName, subDistID,
		nullStr(row.CensusCode), nullStr(row.Pincode),
		lat, lon, pop,
	).Scan(&id, &isNew)

	if err == sql.ErrNoRows {
		return false, nil // already existed
	}
	return isNew, err
}

// nullStr returns nil if s is empty so Postgres stores NULL instead of ""
func nullStr(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

// ─────────────────────────────────────────────────────────────────────────────
// Typesense helpers  (using raw HTTP — avoids SDK version coupling)
// ─────────────────────────────────────────────────────────────────────────────

const tsCollection = "villages"

// typesenseCollectionSchema matches the Typesense REST API schema definition
var typesenseCollectionSchema = map[string]interface{}{
	"name": tsCollection,
	"fields": []map[string]interface{}{
		{"name": "id",               "type": "string"},
		{"name": "village_name",     "type": "string"},
		{"name": "sub_district_name","type": "string"},
		{"name": "district_name",    "type": "string"},
		{"name": "state_name",       "type": "string"},
		{"name": "full_address",     "type": "string"},
		{"name": "village_lgd",      "type": "string", "facet": false},
		{"name": "pincode",          "type": "string", "optional": true},
		{"name": "latitude",         "type": "float",  "optional": true},
		{"name": "longitude",        "type": "float",  "optional": true},
		{"name": "population",       "type": "int64"},  // NOT optional — required by default_sorting_field
	},
	"default_sorting_field": "population",
}

// pingTypesense performs a GET /health and returns an error if Typesense is
// unreachable or reports an unhealthy status.
func pingTypesense(baseURL, apiKey string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", baseURL+"/health", nil)
	req.Header.Set("X-TYPESENSE-API-KEY", apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("health GET: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned HTTP %d", resp.StatusCode)
	}
	log.Println("Typesense connected ✓")
	return nil
}

// ensureTypesenseCollection creates the villages collection if it doesn't exist.
func ensureTypesenseCollection(baseURL, apiKey string) error {
	client := &http.Client{Timeout: 10 * time.Second}

	// Check if already exists
	req, _ := http.NewRequest("GET", baseURL+"/collections/"+tsCollection, nil)
	req.Header.Set("X-TYPESENSE-API-KEY", apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("typesense health check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		log.Printf("Typesense collection '%s' already exists — skipping creation", tsCollection)
		return nil
	}

	// Create the collection
	body, _ := json.Marshal(typesenseCollectionSchema)
	req, _ = http.NewRequest("POST", baseURL+"/collections", strings.NewReader(string(body)))
	req.Header.Set("X-TYPESENSE-API-KEY", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("create collection returned HTTP %d", resp.StatusCode)
	}

	log.Printf("Typesense collection '%s' created ✓", tsCollection)
	return nil
}

// indexTypesense bulk-imports all village documents using Typesense's JSONL import API.
func indexTypesense(baseURL, apiKey string, docs []VillageDoc) error {
	if len(docs) == 0 {
		return nil
	}

	// Build JSONL payload (one JSON object per line)
	var sb strings.Builder
	for _, d := range docs {
		line, err := json.Marshal(d)
		if err != nil {
			return err
		}
		sb.Write(line)
		sb.WriteByte('\n')
	}

	url := fmt.Sprintf("%s/collections/%s/documents/import?action=upsert", baseURL, tsCollection)
	req, _ := http.NewRequest("POST", url, strings.NewReader(sb.String()))
	req.Header.Set("X-TYPESENSE-API-KEY", apiKey)
	req.Header.Set("Content-Type", "text/plain")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("typesense import request: %w", err)
	}
	defer resp.Body.Close()

	// Typesense returns one JSON result per line — check for errors
	respBody, _ := io.ReadAll(resp.Body)
	lines := strings.Split(strings.TrimSpace(string(respBody)), "\n")

	successCount := 0
	for i, line := range lines {
		if line == "" {
			continue
		}
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			log.Printf("  WARN: could not parse Typesense response line %d: %s", i+1, line)
			continue
		}
		if success, ok := result["success"].(bool); ok && success {
			successCount++
		} else {
			log.Printf("  WARN: Typesense index failed for doc %d: %s", i+1, line)
		}
	}

	log.Printf("\n── Typesense Summary ────────────────────────────────")
	log.Printf("   Indexed Villages: %d / %d documents upserted ✓", successCount, len(docs))
	for _, d := range docs {
		log.Printf("  ★ Indexed Village  : %s", d.FullAddress)
	}
	log.Println("─────────────────────────────────────────────────────")

	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// CSV reader
// ─────────────────────────────────────────────────────────────────────────────

// readCSV opens the CSV, validates the header, and returns all data rows.
func readCSV(path string) ([]CSVRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.TrimLeadingSpace = true

	// Read header
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	// Build a column-index map so we're not brittle to column reordering
	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		colIdx[strings.TrimSpace(h)] = i
	}

	required := []string{
		"state_lgd_code", "state_name",
		"district_lgd_code", "district_name",
		"sub_district_lgd_code", "sub_district_name",
		"village_lgd_code", "village_name",
	}
	for _, col := range required {
		if _, ok := colIdx[col]; !ok {
			return nil, fmt.Errorf("missing required column: %q", col)
		}
	}

	var rows []CSVRow
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

		get := func(col string) string {
			i, ok := colIdx[col]
			if !ok || i >= len(record) {
				return ""
			}
			return strings.TrimSpace(record[i])
		}

		rows = append(rows, CSVRow{
			StateLGD:        get("state_lgd_code"),
			StateName:       get("state_name"),
			DistrictLGD:     get("district_lgd_code"),
			DistrictName:    get("district_name"),
			SubDistrictLGD:  get("sub_district_lgd_code"),
			SubDistrictName: get("sub_district_name"),
			VillageLGD:      get("village_lgd_code"),
			VillageName:     get("village_name"),
			CensusCode:      get("census_code"),
			Pincode:         get("pincode"),
			Latitude:        get("latitude"),
			Longitude:       get("longitude"),
			Population:      get("population"),
		})
	}

	return rows, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Document builder
// ─────────────────────────────────────────────────────────────────────────────

func buildDoc(row CSVRow) VillageDoc {
	lat, _ := strconv.ParseFloat(strings.TrimSpace(row.Latitude), 64)
	lon, _ := strconv.ParseFloat(strings.TrimSpace(row.Longitude), 64)
	pop, _ := strconv.ParseInt(strings.TrimSpace(row.Population), 10, 64)

	full := fmt.Sprintf("%s, %s, %s, %s, India",
		row.VillageName, row.SubDistrictName, row.DistrictName, row.StateName)

	return VillageDoc{
		ID:              row.VillageLGD, // Typesense requires string id
		VillageName:     row.VillageName,
		SubDistrictName: row.SubDistrictName,
		DistrictName:    row.DistrictName,
		StateName:       row.StateName,
		FullAddress:     full,
		VillageLGD:      row.VillageLGD,
		Pincode:         row.Pincode,
		Latitude:        lat,
		Longitude:       lon,
		Population:      pop,
	}
}
