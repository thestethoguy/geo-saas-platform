package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"geo-saas-platform/internal/cache"
	"geo-saas-platform/internal/config"
	"geo-saas-platform/internal/database"
	"geo-saas-platform/internal/handlers"
	apimw "geo-saas-platform/internal/middleware"
	"geo-saas-platform/internal/search"
)

func main() {
	// ── 1. Load configuration ─────────────────────────────────────────────
	cfg := config.Load()

	log.Printf("[main] Starting Geo SaaS API | env=%s port=%s", cfg.AppEnv, cfg.AppPort)

	// ── 2. Connect to PostgreSQL ──────────────────────────────────────────
	db, err := database.NewPostgresDB(cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("[main] FATAL: could not connect to PostgreSQL: %v", err)
	}
	defer db.Close()

	// ── 3. Connect to Redis ───────────────────────────────────────────────
	redisClient, err := cache.NewRedisClient(cfg.RedisURL)
	if err != nil {
		log.Fatalf("[main] FATAL: could not connect to Redis: %v", err)
	}
	defer redisClient.Close()

	// ── 4. Connect to Typesense ───────────────────────────────────────────
	searchClient, err := search.NewTypesenseClient(
		cfg.TypesenseURL(),
		cfg.TypesenseAPIKey,
	)
	if err != nil {
		// Non-fatal: the geo-hierarchy endpoints don't need Typesense.
		// /api/v1/search will return 503 until connectivity is restored.
		log.Printf("[main] WARN: Typesense unavailable (%v) — search endpoint will return 503", err)
	}

	// ── 5. Instantiate handlers ───────────────────────────────────────────
	geoHandler    := handlers.NewGeoHandler(db, redisClient)
	searchHandler := handlers.NewSearchHandler(searchClient)

	// ── 6. Build HTTP router ──────────────────────────────────────────────
	r := chi.NewRouter()

	// ── Middleware stack ──────────────────────────────────────────────────
	// CORS must come first so preflight OPTIONS requests are answered before
	// any auth or rate-limit middleware runs.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://127.0.0.1:3000", "https://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "Accept", "X-Request-Id"},
		ExposedHeaders:   []string{"X-Cache", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"},
		AllowCredentials: false,
		MaxAge:           300, // preflight cache: 5 minutes
	}))
	r.Use(middleware.RequestID)                // injects X-Request-Id header
	r.Use(middleware.RealIP)                   // trusts X-Real-IP / X-Forwarded-For
	r.Use(middleware.Logger)                   // structured access log to stdout
	r.Use(middleware.Recoverer)                // catches panics, returns 500
	r.Use(middleware.Timeout(30 * time.Second)) // hard request timeout

	// ── Health check — required by load balancers & k8s probes ───────────
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		if err := db.PingContext(ctx); err != nil {
			http.Error(w, `{"status":"degraded","db":"unreachable"}`, http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok","db":"connected"}`)
	})

	// ── Versioned API group ───────────────────────────────────────────────
	r.Route("/api/v1", func(r chi.Router) {
		// Liveness ping — intentionally unauthenticated for smoke tests
		r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"message":"Geo SaaS API is running 🚀"}`)
		})

		// ── Protected sub-group: Auth + Rate Limiting ─────────────────────
		// Every endpoint below requires a valid Bearer token and is rate-limited
		// based on the authenticated tier (free: 5 req/min, premium: 100 req/min).
		r.Group(func(r chi.Router) {
			r.Use(apimw.NewAuth(db, redisClient))
			r.Use(apimw.NewRateLimit(redisClient))

			// ── Geo hierarchy endpoints ──────────────────────────────────────
			// Returns all active states (cached 24 h under key "api:states")
			r.Get("/states", geoHandler.ListStates)

			// Returns all districts that belong to a given state LGD code
			r.Get("/states/{state_code}/districts", geoHandler.ListDistricts)

			// Returns all sub-districts that belong to a given district LGD code
			r.Get("/districts/{district_code}/sub-districts", geoHandler.ListSubDistricts)

			// Returns all villages that belong to a given sub-district LGD code
			r.Get("/sub-districts/{sub_district_code}/villages", geoHandler.ListVillages)

			// ── Search endpoint (Typesense — no Redis cache) ──────────────────
			// GET /api/v1/search?q=<query>[&limit=<n>]
			// Typo-tolerant, prefix-aware full-text search across all villages.
			r.Get("/search", searchHandler.Search)
		})
	})

	// ── 7. Start HTTP server with graceful shutdown ───────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.AppPort,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Channel to listen for OS termination signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine so we don't block
	go func() {
		log.Printf("[main] HTTP server listening on http://localhost:%s", cfg.AppPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[main] FATAL: ListenAndServe: %v", err)
		}
	}()

	// Block until a shutdown signal is received
	<-quit
	log.Println("[main] Shutdown signal received. Draining connections ...")

	// Give active requests up to 15 seconds to complete
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("[main] FATAL: graceful shutdown failed: %v", err)
	}

	log.Println("[main] Server stopped cleanly ✓")
}
