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
// Helper: writeJSON
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

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/v1/states
// ─────────────────────────────────────────────────────────────────────────────

func (h *GeoHandler) ListStates(w http.ResponseWriter, r *http.Request) {
	const cacheKey = "api:states"

	// 1. Cache hit?
	if cached, err := h.cache.Get(r.Context(), cacheKey); err == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cached))
		return
	} else if !errors.Is(err, redis.Nil) {
		log.Printf("[states] redis GET error: %v", err)
		// Non-fatal — fall through to Postgres
	}

	// 2. Query Postgres
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	rows, err := h.db.QueryContext(ctx, `
		SELECT id, lgd_code, name, COALESCE(name_hi, '')
		FROM   states
		WHERE  is_active = TRUE
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
		if err := rows.Scan(&s.ID, &s.LGDCode, &s.Name, &s.NameHi); err != nil {
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

	// 3. Populate cache
	if b, err := json.Marshal(resp); err == nil {
		if err := h.cache.Set(r.Context(), cacheKey, string(b), cacheTTL); err != nil {
			log.Printf("[states] redis SET error: %v", err)
		}
	}

	// 4. Respond
	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, http.StatusOK, resp)
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/v1/states/{state_code}/districts
// ─────────────────────────────────────────────────────────────────────────────

func (h *GeoHandler) ListDistricts(w http.ResponseWriter, r *http.Request) {
	stateCode := chi.URLParam(r, "state_code")
	if stateCode == "" {
		writeError(w, http.StatusBadRequest, "state_code is required")
		return
	}
	cacheKey := "api:districts:" + stateCode

	// 1. Cache hit?
	if cached, err := h.cache.Get(r.Context(), cacheKey); err == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cached))
		return
	} else if !errors.Is(err, redis.Nil) {
		log.Printf("[districts] redis GET error: %v", err)
	}

	// 2. Verify the state exists
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var stateID int
	err := h.db.QueryRowContext(ctx,
		`SELECT id FROM states WHERE lgd_code = $1 AND is_active = TRUE`, stateCode).
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

	// 3. Fetch districts
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, lgd_code, state_id, name, COALESCE(name_hi, '')
		FROM   districts
		WHERE  state_id = $1 AND is_active = TRUE
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
		if err := rows.Scan(&d.ID, &d.LGDCode, &d.StateID, &d.Name, &d.NameHi); err != nil {
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

	// 4. Populate cache
	if b, err := json.Marshal(resp); err == nil {
		if err := h.cache.Set(r.Context(), cacheKey, string(b), cacheTTL); err != nil {
			log.Printf("[districts] redis SET error: %v", err)
		}
	}

	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, http.StatusOK, resp)
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/v1/districts/{district_code}/sub-districts
// ─────────────────────────────────────────────────────────────────────────────

func (h *GeoHandler) ListSubDistricts(w http.ResponseWriter, r *http.Request) {
	districtCode := chi.URLParam(r, "district_code")
	if districtCode == "" {
		writeError(w, http.StatusBadRequest, "district_code is required")
		return
	}
	cacheKey := "api:sub_districts:" + districtCode

	// 1. Cache hit?
	if cached, err := h.cache.Get(r.Context(), cacheKey); err == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cached))
		return
	} else if !errors.Is(err, redis.Nil) {
		log.Printf("[sub_districts] redis GET error: %v", err)
	}

	// 2. Verify the district exists
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var districtID int
	err := h.db.QueryRowContext(ctx,
		`SELECT id FROM districts WHERE lgd_code = $1 AND is_active = TRUE`, districtCode).
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

	// 3. Fetch sub-districts
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, lgd_code, district_id, name, COALESCE(name_hi, '')
		FROM   sub_districts
		WHERE  district_id = $1 AND is_active = TRUE
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
		if err := rows.Scan(&sd.ID, &sd.LGDCode, &sd.DistrictID, &sd.Name, &sd.NameHi); err != nil {
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

	// 4. Populate cache
	if b, err := json.Marshal(resp); err == nil {
		if err := h.cache.Set(r.Context(), cacheKey, string(b), cacheTTL); err != nil {
			log.Printf("[sub_districts] redis SET error: %v", err)
		}
	}

	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, http.StatusOK, resp)
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/v1/sub-districts/{sub_district_code}/villages
// ─────────────────────────────────────────────────────────────────────────────

func (h *GeoHandler) ListVillages(w http.ResponseWriter, r *http.Request) {
	subDistrictCode := chi.URLParam(r, "sub_district_code")
	if subDistrictCode == "" {
		writeError(w, http.StatusBadRequest, "sub_district_code is required")
		return
	}
	cacheKey := "api:villages:" + subDistrictCode

	// 1. Cache hit?
	if cached, err := h.cache.Get(r.Context(), cacheKey); err == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cached))
		return
	} else if !errors.Is(err, redis.Nil) {
		log.Printf("[villages] redis GET error: %v", err)
	}

	// 2. Verify the sub-district exists
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var subDistrictID int
	err := h.db.QueryRowContext(ctx,
		`SELECT id FROM sub_districts WHERE lgd_code = $1 AND is_active = TRUE`, subDistrictCode).
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

	// 3. Fetch villages
	// Nullable columns (latitude, longitude, population) are scanned into pointers.
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, lgd_code, sub_district_id, name,
		       COALESCE(name_hi, ''),
		       COALESCE(census_code, ''),
		       COALESCE(pincode, ''),
		       latitude, longitude, population
		FROM   villages
		WHERE  sub_district_id = $1 AND is_active = TRUE
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
		if err := rows.Scan(
			&v.ID, &v.LGDCode, &v.SubDistrictID, &v.Name,
			&v.NameHi, &v.CensusCode, &v.Pincode,
			&v.Latitude, &v.Longitude, &v.Population,
		); err != nil {
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

	// 4. Populate cache
	if b, err := json.Marshal(resp); err == nil {
		if err := h.cache.Set(r.Context(), cacheKey, string(b), cacheTTL); err != nil {
			log.Printf("[villages] redis SET error: %v", err)
		}
	}

	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, http.StatusOK, resp)
}
