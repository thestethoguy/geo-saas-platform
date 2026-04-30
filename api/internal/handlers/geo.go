package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"geo-saas-platform/internal/cache"
	"geo-saas-platform/internal/models"
)

// cacheTTL is how long geo hierarchy responses are stored in Redis.
// 24 hours is appropriate: LGD codes change infrequently (government updates).
const cacheTTL = 24 * time.Hour

// GeoHandler holds shared dependencies for every geo endpoint.
type GeoHandler struct {
	db    *sql.DB
	cache *cache.Client
}

// NewGeoHandler constructs a GeoHandler.
func NewGeoHandler(db *sql.DB, c *cache.Client) *GeoHandler {
	return &GeoHandler{db: db, cache: c}
}

// ─────────────────────────────────────────────────────────────────────────────
// JSON helpers
// ─────────────────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[handler] json encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, models.ErrorResponse{Error: msg})
}

// cacheHit writes a cached JSON blob directly to the response, bypassing
// re-serialisation. Returns true if a cache hit was served.
func (h *GeoHandler) cacheHit(w http.ResponseWriter, r *http.Request, key string) bool {
	cached, err := h.cache.Get(r.Context(), key)
	if err == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cached))
		return true
	}
	if !errors.Is(err, redis.Nil) {
		log.Printf("[handler] redis GET error (key=%s): %v", key, err)
		// Non-fatal — fall through to Postgres
	}
	return false
}

// cacheSet serialises v to JSON and stores it in Redis under key.
func (h *GeoHandler) cacheSet(ctx context.Context, key string, v interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("[handler] json marshal error (key=%s): %v", key, err)
		return
	}
	if err := h.cache.Set(ctx, key, string(b), cacheTTL); err != nil {
		log.Printf("[handler] redis SET error (key=%s): %v", key, err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/v1/states
// Returns all states ordered alphabetically.
// Cache key: "api:v1:states"  TTL: 24 h
// ─────────────────────────────────────────────────────────────────────────────

func (h *GeoHandler) ListStates(w http.ResponseWriter, r *http.Request) {
	const cacheKey = "api:v1:states"

	if h.cacheHit(w, r, cacheKey) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	rows, err := h.db.QueryContext(ctx, `
		SELECT id, name, lgd_code
		FROM   states
		ORDER  BY name`)
	if err != nil {
		log.Printf("[states] db error: %v", err)
		writeError(w, http.StatusInternalServerError, "database query failed")
		return
	}
	defer rows.Close()

	states := make([]models.State, 0)
	for rows.Next() {
		var s models.State
		if err := rows.Scan(&s.ID, &s.Name, &s.LGDCode); err != nil {
			log.Printf("[states] scan error: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to read row")
			return
		}
		states = append(states, s)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[states] rows error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to iterate rows")
		return
	}

	resp := models.APIResponse{Count: len(states), Data: states}
	h.cacheSet(r.Context(), cacheKey, resp)

	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, http.StatusOK, resp)
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/v1/states/{state_code}/districts
// Cache key: "api:v1:districts:<state_lgd_code>"
// ─────────────────────────────────────────────────────────────────────────────

func (h *GeoHandler) ListDistricts(w http.ResponseWriter, r *http.Request) {
	stateCode := chi.URLParam(r, "state_code")
	if stateCode == "" {
		writeError(w, http.StatusBadRequest, "state_code is required")
		return
	}
	cacheKey := "api:v1:districts:" + stateCode

	if h.cacheHit(w, r, cacheKey) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Resolve state UUID from lgd_code
	var stateID string
	err := h.db.QueryRowContext(ctx,
		`SELECT id FROM states WHERE lgd_code = $1`, stateCode).
		Scan(&stateID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "state not found")
		return
	}
	if err != nil {
		log.Printf("[districts] state lookup error: %v", err)
		writeError(w, http.StatusInternalServerError, "database query failed")
		return
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT id, state_id, name, lgd_code
		FROM   districts
		WHERE  state_id = $1
		ORDER  BY name`, stateID)
	if err != nil {
		log.Printf("[districts] db error: %v", err)
		writeError(w, http.StatusInternalServerError, "database query failed")
		return
	}
	defer rows.Close()

	districts := make([]models.District, 0)
	for rows.Next() {
		var d models.District
		if err := rows.Scan(&d.ID, &d.StateID, &d.Name, &d.LGDCode); err != nil {
			log.Printf("[districts] scan error: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to read row")
			return
		}
		districts = append(districts, d)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[districts] rows error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to iterate rows")
		return
	}

	resp := models.APIResponse{Count: len(districts), Data: districts}
	h.cacheSet(r.Context(), cacheKey, resp)

	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, http.StatusOK, resp)
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/v1/districts/{district_code}/sub-districts
// Cache key: "api:v1:sub_districts:<district_lgd_code>"
// ─────────────────────────────────────────────────────────────────────────────

func (h *GeoHandler) ListSubDistricts(w http.ResponseWriter, r *http.Request) {
	districtCode := chi.URLParam(r, "district_code")
	if districtCode == "" {
		writeError(w, http.StatusBadRequest, "district_code is required")
		return
	}
	cacheKey := "api:v1:sub_districts:" + districtCode

	if h.cacheHit(w, r, cacheKey) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Resolve district UUID from lgd_code
	var districtID string
	err := h.db.QueryRowContext(ctx,
		`SELECT id FROM districts WHERE lgd_code = $1`, districtCode).
		Scan(&districtID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "district not found")
		return
	}
	if err != nil {
		log.Printf("[sub_districts] district lookup error: %v", err)
		writeError(w, http.StatusInternalServerError, "database query failed")
		return
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT id, district_id, name, lgd_code
		FROM   sub_districts
		WHERE  district_id = $1
		ORDER  BY name`, districtID)
	if err != nil {
		log.Printf("[sub_districts] db error: %v", err)
		writeError(w, http.StatusInternalServerError, "database query failed")
		return
	}
	defer rows.Close()

	subDistricts := make([]models.SubDistrict, 0)
	for rows.Next() {
		var sd models.SubDistrict
		if err := rows.Scan(&sd.ID, &sd.DistrictID, &sd.Name, &sd.LGDCode); err != nil {
			log.Printf("[sub_districts] scan error: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to read row")
			return
		}
		subDistricts = append(subDistricts, sd)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[sub_districts] rows error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to iterate rows")
		return
	}

	resp := models.APIResponse{Count: len(subDistricts), Data: subDistricts}
	h.cacheSet(r.Context(), cacheKey, resp)

	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, http.StatusOK, resp)
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/v1/sub-districts/{sub_district_code}/villages
// Cache key: "api:v1:villages:<sub_district_lgd_code>"
// ─────────────────────────────────────────────────────────────────────────────

func (h *GeoHandler) ListVillages(w http.ResponseWriter, r *http.Request) {
	subDistrictCode := chi.URLParam(r, "sub_district_code")
	if subDistrictCode == "" {
		writeError(w, http.StatusBadRequest, "sub_district_code is required")
		return
	}
	cacheKey := "api:v1:villages:" + subDistrictCode

	if h.cacheHit(w, r, cacheKey) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Resolve sub-district UUID from lgd_code
	var subDistrictID string
	err := h.db.QueryRowContext(ctx,
		`SELECT id FROM sub_districts WHERE lgd_code = $1`, subDistrictCode).
		Scan(&subDistrictID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "sub-district not found")
		return
	}
	if err != nil {
		log.Printf("[villages] sub-district lookup error: %v", err)
		writeError(w, http.StatusInternalServerError, "database query failed")
		return
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT id, sub_district_id, name, lgd_code
		FROM   villages
		WHERE  sub_district_id = $1
		ORDER  BY name`, subDistrictID)
	if err != nil {
		log.Printf("[villages] db error: %v", err)
		writeError(w, http.StatusInternalServerError, "database query failed")
		return
	}
	defer rows.Close()

	villages := make([]models.Village, 0)
	for rows.Next() {
		var v models.Village
		if err := rows.Scan(&v.ID, &v.SubDistrictID, &v.Name, &v.LGDCode); err != nil {
			log.Printf("[villages] scan error: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to read row")
			return
		}
		villages = append(villages, v)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[villages] rows error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to iterate rows")
		return
	}

	resp := models.APIResponse{Count: len(villages), Data: villages}
	h.cacheSet(r.Context(), cacheKey, resp)

	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, http.StatusOK, resp)
}
