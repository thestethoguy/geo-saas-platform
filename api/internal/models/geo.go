package models

// ─────────────────────────────────────────────────────────────────────────────
// V1 Geo Models — UUID primary keys, strict 4-level hierarchy
//
//   State → District → SubDistrict → Village
//
// Fields mirror the 003_v1_production_schema.sql columns exactly.
// ─────────────────────────────────────────────────────────────────────────────

// State represents a row from the `states` table.
type State struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	LGDCode string `json:"lgd_code"`
}

// District represents a row from the `districts` table.
type District struct {
	ID      string `json:"id"`
	StateID string `json:"state_id"`
	Name    string `json:"name"`
	LGDCode string `json:"lgd_code"`
}

// SubDistrict represents a row from the `sub_districts` table.
type SubDistrict struct {
	ID         string `json:"id"`
	DistrictID string `json:"district_id"`
	Name       string `json:"name"`
	LGDCode    string `json:"lgd_code"`
}

// Village represents a row from the `villages` table.
type Village struct {
	ID            string `json:"id"`
	SubDistrictID string `json:"sub_district_id"`
	Name          string `json:"name"`
	LGDCode       string `json:"lgd_code"`
}

// ─────────────────────────────────────────────────────────────────────────────
// API envelope types (shared across all handlers)
// ─────────────────────────────────────────────────────────────────────────────

// APIResponse is the standard envelope for all list endpoints.
//
//	{ "count": 5, "data": [...] }
type APIResponse struct {
	Count int         `json:"count"`
	Data  interface{} `json:"data"`
}

// ErrorResponse is the standard error envelope.
//
//	{ "error": "state not found" }
type ErrorResponse struct {
	Error string `json:"error"`
}
