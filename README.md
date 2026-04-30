# GeoData SaaS Platform
> A production-grade, high-speed REST API...

🌐 **Live Demo:** [https://geo-saas-platform.vercel.app/](https://geo-saas-platform.vercel.app/) &nbsp;|&nbsp; 
🚀 **API:** [https://geo-api-7ngv.onrender.com](https://geo-api-7ngv.onrender.com)
<br>

---

## Table of Contents

1. [Overview of the Project](#1-overview-of-the-project)
2. [Working Modules and Implemented Features](#2-working-modules-and-implemented-features)
3. [Explanation of the Frontend Module](#3-explanation-of-the-frontend-module)
4. [Code Logic and Architecture](#4-code-logic-and-architecture)
5. [Pending Work / Future Improvements](#5-pending-work--future-improvements)

---

<br>

## 1. Overview of the Project

### What Is GeoData SaaS Platform?

India's administrative geography is vast and deeply hierarchical — **36 states and union territories**, **800+ districts**, **7,000+ sub-districts**, **640,000+ villages**, and thousands of census towns, all formally coded under the **Local Government Directory (LGD)** system maintained by the Ministry of Panchayati Raj. This dataset is the canonical reference for government applications, logistics systems, public health mapping, and civic tech — yet it has historically been locked away in unwieldy Excel sheets and slow government portals that were never designed for programmatic access.

**GeoData SaaS Platform** solves this directly. It ingests, indexes, and exposes the entire LGD geographic hierarchy through a **clean, authenticated REST API** built for developers, paired with a **visual dashboard** that lets users test the API in real time — all without a single millisecond of avoidable latency.

### The Core Problem

When a food-delivery app needs to validate a delivery pin, when a health worker app needs to autocomplete a village name mid-form, or when a fintech platform needs to resolve a district code to a state — they all face the same problem: the data is scattered, unstructured, and slow. Traditional database `LIKE` queries over half a million records are expensive and typo-intolerant. A user typing `"Hinjewadi"` slightly wrong as `"Hinjwadi"` would get zero results from a naïve SQL query.

GeoData SaaS Platform addresses this with:

- **Fuzzy, typo-tolerant full-text search** via Typesense, returning accurate results in **under 5 milliseconds**
- **564,000+ indexed geographic locations** covering villages, towns, sub-districts, districts, and states
- **LGD-code-aware data model** that preserves the full administrative hierarchy
- **A developer-first API** with Bearer token authentication, rate limiting, and a structured JSON response contract

### Technology Stack at a Glance

| Layer | Technology | Purpose |
|---|---|---|
| **API Server** | Go (Golang) | High-performance REST API |
| **Search Engine** | Typesense (Render) | Sub-5ms fuzzy geographic search |
| **Primary Database** | Neon (Serverless PostgreSQL) | Persistent storage, API key management |
| **Cache / Rate Limiter** | Upstash (Redis / Valkey) | Real-time request rate limiting |
| **Frontend** | Next.js 14, React 18, Tailwind CSS | Visual dashboard and API Tester |
| **API Deployment** | Render (Dockerized container) | Scalable, zero-downtime Go API hosting |
| **Frontend Deployment** | Vercel | Edge-optimized Next.js deployment |
| **ETL / Ingestion** | Go scripts (`scripts/ingest/`) | CSV → PostgreSQL + Typesense pipeline |

### Deployment Strategy

The platform follows a **polyglot cloud** deployment model, deliberately placing each service on the infrastructure best suited to it:

- The **Go API** runs as a Dockerized container on **Render**, allowing horizontal scaling and reproducible builds via `docker-compose` during local development.
- **Typesense** is self-hosted on Render as a dedicated service, giving full control over the search engine's configuration and API key.
- **Neon** provides serverless PostgreSQL — its connection pooling is ideal for the Go API's stateless, burst-traffic nature.
- **Upstash** Redis operates on a serverless model with per-request pricing, making it cost-efficient for rate-limiting workloads that are inherently spiky.
- The **Next.js frontend** is deployed to **Vercel**, which provides automatic edge caching, preview deployments per pull request, and zero-configuration CI/CD.

<br>

---

## 2. Working Modules and Implemented Features

### 2.1 Data Ingestion Module

The foundation of the entire platform is data quality and completeness. The ingestion pipeline is a standalone Go program located at `scripts/ingest/main.go`, invoked via:

```bash
make ingest
```

**What it does, step by step:**

1. **Reads the raw LGD CSV dataset** — the source file contains all of India's geographic entities with their LGD codes, parent codes, entity type (village / town / sub-district / district / state), and names in English.

2. **Establishes a connection to Neon PostgreSQL** using environment variables (`DATABASE_URL` in production, or individual `POSTGRES_*` vars for local dev loaded via `godotenv`).

3. **Bulk-inserts all records into the relational database** using the `lib/pq` PostgreSQL driver. The database schema preserves the full LGD hierarchy, enabling relational queries like "all villages in Pune district."

4. **Concurrently indexes every record into Typesense** by making batch import API calls to the Typesense HTTP endpoint. Each document is structured as a JSON object with fields such as `lgd_code`, `name`, `entity_type`, `parent_lgd_code`, and `state_name`.

5. **Reports ingestion statistics** on completion — total records written, any skipped/malformed rows, and elapsed time.

The ingestion script is **idempotent by design**: running it multiple times will upsert rather than duplicate, making it safe for re-runs after CSV corrections.

**Key dependencies for ingestion (`scripts/ingest/go.mod`):**
- `github.com/lib/pq` — PostgreSQL wire protocol driver
- `github.com/joho/godotenv` — `.env` file loading for local execution

---

### 2.2 Authentication & Security

Every API endpoint (except a public `/health` check) is protected by **Bearer token authentication** backed by the Neon PostgreSQL database.

**How it works:**

- When a developer signs up on the dashboard, a cryptographically random API key is generated and stored in the `api_keys` table in Neon PostgreSQL.
- Every inbound HTTP request must carry this key in the `Authorization` header:

```http
Authorization: Bearer <your-api-key>
```

- The Go API's authentication middleware (`internal/middleware/auth.go`) extracts the token from the header, queries the `api_keys` table to validate its existence and active status, and either allows the request to proceed or returns a structured `401 Unauthorized` response.

```json
{
  "error": "invalid_token",
  "message": "The provided API key is not recognized or has been revoked."
}
```

- **Database-backed validation** means keys can be revoked instantly by flipping a status flag — no JWT expiry windows, no token refresh complexity.
- Auth lookups are intentionally lightweight: the query is a simple primary-key lookup with a covering index, keeping auth overhead negligible even under high concurrency.

---

### 2.3 Rate Limiting

To protect the platform from abuse and to enforce fair-usage tiers, every authenticated request passes through a **Redis-based rate limiter** powered by **Upstash (Redis / Valkey)**.

**Implementation details:**

The rate limiter is implemented using the **sliding window counter** algorithm:

- Each API key gets its own Redis key namespaced as `ratelimit:<api_key>`.
- On every request, the middleware increments the counter for the current time window and checks it against the configured limit (e.g., `100 requests / 60 seconds`).
- Upstash's serverless Redis guarantees sub-millisecond counter operations, ensuring rate limiting adds no perceptible latency to the request path.

**Response headers** are attached to every API response, giving clients real-time visibility into their quota:

```http
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 73
X-RateLimit-Reset: 1717000860
```

When a client exceeds their limit, the API returns:

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 47
```

```json
{
  "error": "rate_limit_exceeded",
  "message": "You have exceeded your request quota. Please retry after 47 seconds."
}
```

The rate limit configuration is environment-driven, making it trivial to set different tiers (free vs. paid) without code changes.

---

### 2.4 High-Speed Search via Typesense

The search layer is the core value proposition of the platform. **Typesense** is a modern, open-source search engine purpose-built for low-latency, typo-tolerant search over structured datasets — making it an ideal fit for geographic name resolution.

**Typesense collection schema (geographic entities):**

```json
{
  "name": "geo_locations",
  "fields": [
    { "name": "lgd_code",       "type": "string" },
    { "name": "name",           "type": "string"  },
    { "name": "entity_type",    "type": "string",  "facet": true },
    { "name": "district_name",  "type": "string",  "facet": true },
    { "name": "state_name",     "type": "string",  "facet": true },
    { "name": "parent_lgd_code","type": "string" }
  ],
  "default_sorting_field": "name"
}
```

**Search capabilities exposed by the API:**

| Feature | Detail |
|---|---|
| **Fuzzy matching** | Configurable edit distance (e.g., `"Hinjwadi"` → `"Hinjewadi"`) |
| **Prefix search** | Instant autocomplete as the user types |
| **Faceted filtering** | Filter by `entity_type`, `state_name`, or `district_name` |
| **Ranked results** | Results ranked by relevance score + name frequency |
| **Sub-5ms latency** | Achieved via in-memory HNSW indexing over 564K+ documents |

**Example API call:**

```bash
curl -X GET "https://api.geodata.dev/v1/search?q=hinjwadi&type=village&state=maharashtra" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

**Example response:**

```json
{
  "query": "hinjwadi",
  "found": 3,
  "latency_ms": 2,
  "results": [
    {
      "lgd_code": "573211",
      "name": "Hinjewadi",
      "entity_type": "village",
      "district_name": "Pune",
      "state_name": "Maharashtra",
      "parent_lgd_code": "5732"
    }
  ]
}
```

<br>

---

## 3. Explanation of the Frontend Module

### Overview

The frontend is a **Next.js 14 application** (React 18, App Router) styled entirely with **Tailwind CSS**, deployed to Vercel. It serves two primary audiences: developers evaluating the API and registered users actively consuming it.

The design philosophy is **function-first, aesthetics-second-but-not-ignored** — the dashboard is dark-mode by default (consistent with developer tooling conventions), with a clean information hierarchy and zero visual noise.

---

### 3.1 Visual Design System

The UI is built around a **dark-mode-first design** using Tailwind CSS utility classes with a consistent design language:

- **Color palette:** Deep slate backgrounds (`slate-900`, `slate-800`) with `emerald` and `sky` accent colors for interactive elements and success/info states
- **Typography:** Monospace fonts for API responses and code blocks; sans-serif for UI chrome
- **Component architecture:** Leverages Next.js App Router's Server Components for static shells (navigation, hero sections) and Client Components for interactive elements (search input, API tester)
- **Responsive layout:** Fluid grid system that collapses gracefully from a two-column dashboard on desktop to a single-column stacked layout on mobile

---

### 3.2 The API Tester Dashboard

The centerpiece of the frontend is an **in-browser API Tester** — a developer experience tool that eliminates the need to open Postman or write curl commands just to validate API behavior.

**Dashboard sections:**

**① Token Input Panel**

The user pastes their Bearer token into a secure input field (masked by default, with a show/hide toggle). The token is stored only in React component state — it is never written to `localStorage` or sent anywhere other than the API.

**② Query Builder**

A structured form that constructs a live API request:

```
Search Query:   [ hinjewadi          ]
Entity Type:    [ village ▾          ]
State:          [ maharashtra ▾      ]
Limit:          [ 10                 ]

                      [ 🔍 Search ]
```

**③ Live Response Viewer**

On submission, the frontend calls the Go API with the user's token and query parameters. The raw JSON response is rendered in a syntax-highlighted, formatted code block — making it immediately inspectable.

**④ Request Metadata Strip**

Below the response, a metadata bar displays:

```
Status: 200 OK  ·  Latency: 3ms  ·  Results: 3  ·  Requests used: 27/100
```

This strip is populated from the JSON response body (`latency_ms`, `found`) and the response headers (`X-RateLimit-Remaining`, `X-RateLimit-Limit`).

---

### 3.3 Real-Time Latency Tracking

The frontend calculates **client-side round-trip time** independently from the server-reported Typesense latency:

```javascript
const start = performance.now();
const response = await fetch(`${API_BASE}/v1/search?q=${query}`, { headers });
const clientLatency = Math.round(performance.now() - start);
```

Both values are displayed side by side:

```
Search engine latency: 2ms   |   Total round-trip: 148ms
```

This gives developers a transparent breakdown — they can see exactly how much latency is attributable to the Typesense search itself versus network transit.

---

### 3.4 Rate Limit Visualization

The dashboard renders a **visual rate-limit progress bar** that updates after each request:

```
API Quota:  ████████████░░░░░░░░  27 / 100 requests used
Resets in:  00:47
```

- The bar fills with an `emerald` color up to 75% usage, transitions to `amber` between 75–90%, and turns `red` when above 90%
- A countdown timer shows the exact seconds until the current window resets, derived from the `X-RateLimit-Reset` Unix timestamp in the response header
- When the `429 Too Many Requests` error is returned, the dashboard displays an inline alert with the `Retry-After` value rather than a blank or broken state

<br>

---

## 4. Code Logic and Architecture

### 4.1 Go API Project Structure

The Go API follows the **Standard Go Project Layout**, separating concerns cleanly across directories:

```
api/
├── cmd/
│   └── main.go              # Entry point — wires up config, DB, Redis, Typesense, router
├── internal/
│   ├── config/
│   │   └── config.go        # Loads env vars (DATABASE_URL, REDIS_URL, etc.)
│   ├── db/
│   │   └── postgres.go      # Neon PostgreSQL connection pool
│   ├── cache/
│   │   └── redis.go         # Upstash Redis client initialization
│   ├── search/
│   │   └── typesense.go     # Typesense client, collection bootstrap, search helpers
│   ├── middleware/
│   │   ├── auth.go          # Bearer token validation middleware
│   │   └── ratelimit.go     # Sliding-window rate limiter middleware
│   └── handlers/
│       ├── search.go        # GET /v1/search handler
│       ├── health.go        # GET /health handler (unauthenticated)
│       └── locations.go     # GET /v1/locations/:lgd_code handler
├── go.mod
├── go.sum
└── bin/
    └── server               # Compiled binary (git-ignored)
```

**Architectural pattern:** The `cmd/` directory contains only the wiring/bootstrap code. All business logic lives in `internal/`, which is unexported by Go convention — this package cannot be imported by external modules, enforcing encapsulation.

---

### 4.2 End-to-End Request Lifecycle

The following describes precisely what happens when a user types **"Hinjewadi"** into the dashboard and clicks Search.

---

**Step 1 — Next.js Client Fires the Request**

The React client component captures the input value and constructs a `fetch` call:

```javascript
// web/src/components/ApiTester.tsx (Client Component)
const res = await fetch(
  `${process.env.NEXT_PUBLIC_API_URL}/v1/search?q=${encodeURIComponent(query)}&type=${entityType}&state=${state}&limit=10`,
  {
    method: "GET",
    headers: {
      "Authorization": `Bearer ${apiKey}`,
      "Content-Type": "application/json",
    },
  }
);
```

The request travels from the user's browser → Vercel edge → the Go API on Render.

---

**Step 2 — Go API Receives and Routes the Request**

`cmd/main.go` initializes the HTTP router at startup. An incoming `GET /v1/search` request is matched to the search handler, but first passes through the middleware chain:

```
Inbound request
      │
      ▼
┌─────────────────┐
│  Auth Middleware │  ← internal/middleware/auth.go
└────────┬────────┘
         │
      ▼
┌──────────────────────┐
│  Rate Limit Middleware│  ← internal/middleware/ratelimit.go
└────────┬─────────────┘
         │
      ▼
┌──────────────────┐
│  Search Handler  │  ← internal/handlers/search.go
└──────────────────┘
```

---

**Step 3 — Bearer Token Validation (Neon PostgreSQL)**

Inside `internal/middleware/auth.go`:

```go
token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
if token == "" {
    respondError(w, http.StatusUnauthorized, "missing_token", "Authorization header is required.")
    return
}

var keyID string
err := db.QueryRowContext(ctx,
    `SELECT id FROM api_keys WHERE key = $1 AND status = 'active' LIMIT 1`,
    token,
).Scan(&keyID)

if err == sql.ErrNoRows {
    respondError(w, http.StatusUnauthorized, "invalid_token", "The provided API key is not recognized.")
    return
}
```

If validation fails, the request is terminated here with a `401` — it never reaches the rate limiter or search engine. If it succeeds, the resolved `keyID` is injected into the request context for downstream use.

---

**Step 4 — Rate Limit Check (Upstash Redis)**

Inside `internal/middleware/ratelimit.go`, the sliding window check runs against Upstash:

```go
redisKey := fmt.Sprintf("ratelimit:%s", keyID)
count, err := rdb.Incr(ctx, redisKey).Result()

if count == 1 {
    rdb.Expire(ctx, redisKey, 60*time.Second)
}

w.Header().Set("X-RateLimit-Limit", "100")
w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(100-count, 10))

if count > 100 {
    respondError(w, http.StatusTooManyRequests, "rate_limit_exceeded",
        "You have exceeded your request quota.")
    return
}
```

The Upstash `INCR` + `EXPIRE` pattern is atomic and reliable — even under concurrent requests for the same API key, Redis's single-threaded command execution prevents counter races.

---

**Step 5 — Typesense Search Execution**

Inside `internal/handlers/search.go`, the validated request is forwarded to Typesense:

```go
searchParams := &typesense.SearchCollectionParams{
    Q:        query,
    QueryBy:  "name,district_name,state_name",
    FilterBy: buildFilter(entityType, state),   // e.g. "entity_type:=village && state_name:=Maharashtra"
    NumTypos: "1",
    PerPage:  limit,
    SortBy:   "_text_match:desc",
}

result, err := tsClient.Collection("geo_locations").Documents().Search(ctx, searchParams)
```

Typesense executes a **fuzzy full-text search** across its in-memory HNSW index of 564,000+ documents. The engine applies edit-distance correction (1 typo allowed), scores each match, and returns a ranked result list — all in under 5 milliseconds.

---

**Step 6 — Response Construction and Return**

The handler assembles the final JSON payload, enriching it with metadata:

```go
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(http.StatusOK)
json.NewEncoder(w).Encode(map[string]interface{}{
    "query":      query,
    "found":      *result.Found,
    "latency_ms": *result.SearchTimeMs,
    "results":    formatHits(result.Hits),
})
```

The response travels back to the Next.js client, which updates the React state — re-rendering the JSON viewer, updating the latency strip, and advancing the rate-limit progress bar — all without a page reload.

---

### 4.3 Architecture Diagram

```
┌─────────────────────────────────────────────────────┐
│                    USER BROWSER                      │
│  ┌──────────────────────────────────────────────┐   │
│  │     Next.js Dashboard (Vercel Edge)           │   │
│  │   API Tester · Latency View · Rate Limit UI  │   │
│  └───────────────────┬──────────────────────────┘   │
└──────────────────────┼──────────────────────────────┘
                       │ HTTPS  Bearer <token>
                       ▼
┌─────────────────────────────────────────────────────┐
│            Go REST API (Render Docker)               │
│                                                      │
│  Auth Middleware ──► Neon PostgreSQL (api_keys)      │
│         │                                            │
│  Rate Limit MW  ──► Upstash Redis (counters)        │
│         │                                            │
│  Search Handler ──► Typesense (564K geo index)      │
│                                                      │
│  [ JSON Response + Rate-Limit Headers ]              │
└─────────────────────────────────────────────────────┘
                       │
        ┌──────────────┼──────────────┐
        ▼              ▼              ▼
  Neon PostgreSQL  Upstash Redis  Typesense
  (Serverless PG)  (Valkey)       (Render)
  - api_keys       - ratelimit:*  - geo_locations
  - locations      - counters     - 564K documents
```

<br>

---

## 5. Pending Work / Future Improvements

The platform is production-ready in its current form, but several high-impact features would meaningfully elevate it from a strong developer tool to a full-fledged geographic data infrastructure product.

---

### 5.1 Auto-Generated SDKs (Python, Node.js, Go)

Developers should not need to hand-write HTTP clients. The next logical step is publishing **first-class SDKs** for the three most common server-side languages:

```python
# Example — Python SDK
from geodata import GeoDataClient

client = GeoDataClient(api_key="YOUR_KEY")
results = client.search("Hinjewadi", entity_type="village", state="maharashtra")
print(results[0].lgd_code)  # → "573211"
```

SDKs would be generated from an **OpenAPI 3.0 specification** (which the Go API would emit at `/openapi.json`), using tools like `openapi-generator`. They would handle authentication, retry logic, rate-limit backoff, and response deserialization — turning a multi-step integration into a two-line import.

---

### 5.2 Geospatial Polygon & Radius Search

The current implementation treats geographic entities as named, hierarchically related records — but not as points in space. Enriching the dataset with **latitude/longitude centroids** for each LGD entity would unlock an entirely new class of queries:

```
GET /v1/search/nearby?lat=18.5912&lon=73.7389&radius_km=50&type=village
```

This would allow applications to answer questions like: *"Show me all villages within 50km of Pune city center"* — a critical capability for logistics routing, healthcare zone planning, and disaster response mapping.

**Implementation path:**
- Enrich the Neon PostgreSQL schema with a `GEOGRAPHY(POINT, 4326)` column using **PostGIS**
- Add a `geo_point` field to the Typesense collection (Typesense natively supports geo search)
- Add a `/v1/search/nearby` endpoint to the Go API that passes lat/lon + radius to Typesense's `filter_by` geo operator

---

### 5.3 Granular API Analytics and Usage Billing Dashboard

The current dashboard shows a simple rate-limit counter. A production SaaS product needs deeper instrumentation:

- **Per-key usage analytics:** Requests over time, top query terms, error rates, and p95 latency — all broken down per API key and visualizable in the dashboard
- **Billing integration:** Usage-based pricing tiers (free: 1,000 req/day; pro: 100,000 req/day; enterprise: unlimited) enforced in the rate limiter and exposed in the dashboard with Stripe-powered subscription management
- **Alerting:** Email or webhook notifications when a key approaches its quota, or when a key is used from an unexpected IP range

**Implementation path:**
- Log every API request asynchronously to a `request_logs` table in Neon (non-blocking, via a Go goroutine)
- Expose aggregated analytics via a `/dashboard/analytics` API route consumed by the Next.js frontend
- Integrate **Stripe Billing** for subscription management and quota upgrades

---

### 5.4 GraphQL API Alongside REST

While the REST API serves most use cases well, complex clients — particularly those building multi-level geographic selectors (state → district → sub-district → village) — benefit from GraphQL's ability to fetch precisely shaped data in a single round trip:

```graphql
query {
  location(lgdCode: "573211") {
    name
    entityType
    parent {
      name
      entityType
      parent {
        name
        entityType
      }
    }
  }
}
```

This query traverses the full hierarchy upward from a village to its state in one request — impossible with the current REST model without three sequential HTTP calls.

**Implementation path:**
- Add `gqlgen` (the idiomatic Go GraphQL library) to the API module
- Define a `Location` GraphQL type with a self-referential `parent` resolver backed by recursive Neon queries
- Expose the GraphQL endpoint at `/graphql` alongside the existing `/v1/` REST routes — no breaking changes

<br>

---

## Local Development Setup

### Prerequisites

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (for Postgres, Redis, Typesense)
- [Go 1.22+](https://go.dev/dl/)
- [Node.js 18+](https://nodejs.org/) (for the Next.js frontend)

### 1. Clone and Configure

```bash
git clone https://github.com/thestethoguy/geo-saas-platform.git
cd geo-saas-platform

cp .env.example .env
# Edit .env and fill in your passwords / API keys
```

### 2. Start Infrastructure

```bash
make up
# Starts PostgreSQL (5432), Redis (6379), Typesense (8108)
```

### 3. Run Data Ingestion

```bash
make ingest
# Reads LGD CSV → writes to PostgreSQL + indexes into Typesense
# This populates 564,000+ location records
```

### 4. Start the API Server

```bash
make run
# Go API server starts on http://localhost:8080
```

### 5. Start the Frontend

```bash
cd web
npm install
npm run dev
# Next.js dev server on http://localhost:3000
```

### Available Make Targets

```bash
make help          # List all targets with descriptions
make up            # Start all Docker containers
make down          # Stop all containers
make restart       # down + up
make logs          # Tail all container logs
make db-shell      # Open psql inside the Postgres container
make redis-shell   # Open redis-cli inside the Redis container
make run           # Start Go API server
make build         # Compile Go binary to ./api/bin/server
make tidy          # go mod tidy
make vet           # go vet ./...
make ingest        # Run CSV → DB + Typesense ingestion
make clean         # Remove compiled binary
```

<br>

---

## Environment Variables Reference

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | Prod only | — | Full Neon PostgreSQL connection URL |
| `REDIS_URL` | Prod only | — | Full Upstash Redis connection URL |
| `POSTGRES_USER` | Dev only | `geouser` | Local Postgres username |
| `POSTGRES_PASSWORD` | Dev only | — | Local Postgres password |
| `POSTGRES_DB` | Dev only | `geosaas` | Local Postgres database name |
| `POSTGRES_HOST` | Dev only | `localhost` | Local Postgres host |
| `POSTGRES_PORT` | Dev only | `5432` | Local Postgres port |
| `REDIS_HOST` | Dev only | `localhost` | Local Redis host |
| `REDIS_PORT` | Dev only | `6379` | Local Redis port |
| `REDIS_PASSWORD` | Dev only | — | Local Redis password |
| `TYPESENSE_HOST` | Both | `localhost` | Typesense host |
| `TYPESENSE_PORT` | Both | `8108` | Typesense port |
| `TYPESENSE_PROTOCOL` | Both | `http` | `http` (dev) or `https` (prod) |
| `TYPESENSE_API_KEY` | Both | — | Typesense admin API key |
| `APP_PORT` | Both | `8080` | Go API server port |
| `APP_ENV` | Both | `development` | `development` or `production` |
| `LOG_LEVEL` | Both | `debug` | `debug`, `info`, `warn`, `error` |

<br>

---

## Author

**Aman Aaryan**
📧 [amanaaryan672@gmail.com](mailto:amanaaryan672@gmail.com)
🌐 [Live Demo](https://geo-saas-platform.vercel.app/)
---

*GeoData SaaS Platform — Making India's geographic data accessible, fast, and developer-friendly.*
