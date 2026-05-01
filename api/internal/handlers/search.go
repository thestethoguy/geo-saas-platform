package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/typesense/typesense-go/v4/typesense/api"

	"geo-saas-platform/internal/search"
)

// ─────────────────────────────────────────────────────────────────────────────
// Constants
// ─────────────────────────────────────────────────────────────────────────────

const (
	// tsCollection is the Typesense collection name defined by the ETL script.
	tsCollection = "villages"

	// defaultSearchLimit is returned when the caller omits the ?limit parameter.
	defaultSearchLimit = 10

	// maxSearchLimit guards against unreasonably large result sets.
	maxSearchLimit = 100
)

// ─────────────────────────────────────────────────────────────────────────────
// SearchHandler
// ─────────────────────────────────────────────────────────────────────────────

// SearchHandler holds the Typesense client for the search endpoint.
type SearchHandler struct {
	ts *search.Client
}

// NewSearchHandler constructs a SearchHandler.
func NewSearchHandler(ts *search.Client) *SearchHandler {
	return &SearchHandler{ts: ts}
}

// ─────────────────────────────────────────────────────────────────────────────
// SearchResult — clean response shape exposed to API consumers
// ─────────────────────────────────────────────────────────────────────────────

// SearchResponse is the envelope returned by GET /api/v1/search.
type SearchResponse struct {
	Query        string                   `json:"query"`
	Found        int                      `json:"found"`
	SearchTimeMs int                      `json:"search_time_ms"`
	Results      []map[string]interface{} `json:"results"`
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/v1/search?q=<query>[&limit=<n>]
// ─────────────────────────────────────────────────────────────────────────────

// Search handles full-text, typo-tolerant autocomplete queries against Typesense.
//
// Query parameters:
//
//	q     – required; the search string (e.g. "Koramangala")
//	limit – optional; max results to return (default 10, max 100)
//
// No Redis caching is applied here — Typesense operates in-memory and already
// serves sub-millisecond responses; adding a Redis layer would slow down
// multi-term prefix searches that change on every keystroke.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	// ── 0. Guard: Typesense client is nil when startup connectivity failed ──
	if h.ts == nil {
		// This fires when NewTypesenseClient() failed at boot (Health() timeout).
		// The real upstream error was already logged in cmd/main.go at startup;
		// repeat it here so every failing request is traceable in Render logs.
		log.Printf("[search] ERROR: Typesense client is nil — search unavailable. Check startup logs for the root cause (likely: URL construction or API key issue).")
		writeError(w, http.StatusServiceUnavailable, "search service is currently unavailable — please try again later")
		return
	}

	// ── 1. Parse & validate query parameters ─────────────────────────────
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	limit := defaultSearchLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "'limit' must be a positive integer")
			return
		}
		if n > maxSearchLimit {
			n = maxSearchLimit
		}
		limit = n
	}

	// ── 2. Build Typesense search parameters ─────────────────────────────
	//
	// query_by — weighted field list: village_name is highest priority, then
	//             full_address for a second pass, then the parent-level names.
	// num_typos=2 — allows up to 2 character typos per token (Typesense default
	//               is 2 but we make it explicit for documentation clarity).
	// prefix=true — enables prefix matching so "Koram" matches "Koramangala".
	// prioritize_exact_match=true — bubbles exact-match results to the top.
	queryBy    := "village_name,sub_district_name,district_name,state_name"
	numTypos   := "2"
	prefix     := "true"
	exactMatch := true

	params := &api.SearchCollectionParams{
		Q:                    &q,
		QueryBy:              &queryBy,
		NumTypos:             &numTypos,
		Prefix:               &prefix,
		Limit:                &limit,
		PrioritizeExactMatch: &exactMatch,
	}

	// ── 3. Execute search against Typesense ──────────────────────────────
	result, err := h.ts.TS().Collection(tsCollection).Documents().Search(r.Context(), params)
	if err != nil {
		// Log the full raw error so it appears in Render's log stream.
		// This is the primary diagnostic line for upstream Typesense failures.
		log.Printf("[search] TYPESENSE RAW ERROR: %v", err)
		writeError(w, http.StatusBadGateway, "search service unavailable")
		return
	}

	// ── 4. Extract and flatten hits ───────────────────────────────────────
	//
	// We only return the raw document object from each hit — Typesense-internal
	// fields (text_match scores, highlight snippets, etc.) are deliberately
	// stripped so the API surface stays clean and stable.
	hits := make([]map[string]interface{}, 0)
	if result.Hits != nil {
		for _, hit := range *result.Hits {
			if hit.Document != nil {
				hits = append(hits, *hit.Document)
			}
		}
	}

	// ── 5. Build response envelope ────────────────────────────────────────
	found := 0
	if result.Found != nil {
		found = *result.Found
	}
	searchTimeMs := 0
	if result.SearchTimeMs != nil {
		searchTimeMs = *result.SearchTimeMs
	}

	resp := SearchResponse{
		Query:        q,
		Found:        found,
		SearchTimeMs: searchTimeMs,
		Results:      hits,
	}

	writeJSON(w, http.StatusOK, resp)
}

