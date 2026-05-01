// Package search wraps the Typesense Go client and exposes a clean interface
// used by the HTTP handlers. It is intentionally thin — all search logic lives
// in the handler layer so that parameters flow transparently.
package search

import (
	"context"
	"log"
	"time"

	"github.com/typesense/typesense-go/v4/typesense"
)

// Client wraps the official Typesense client.
type Client struct {
	ts *typesense.Client
}

// NewTypesenseClient creates and validates a Typesense connection.
//
//	serverURL – full URL, e.g. "http://localhost:8108"
//	apiKey    – TYPESENSE_API_KEY
func NewTypesenseClient(serverURL, apiKey string) (*Client, error) {
	ts := typesense.NewClient(
		typesense.WithServer(serverURL),
		typesense.WithAPIKey(apiKey),
		typesense.WithNumRetries(2),
	)

	// Probe connectivity with a best-effort Health() check.
	//
	// ⚠  We intentionally do NOT treat a failed probe as fatal.
	//    Transient errors (429 DDoS-protection blocks, cold-start timeouts,
	//    brief network blips) must not permanently nil the client for the
	//    entire process lifetime.  The client is fully usable regardless of
	//    whether this probe succeeds — it will auto-recover once the block
	//    expires without requiring a redeploy.
	healthy, err := ts.Health(context.Background(), 10*time.Second)
	switch {
	case err != nil:
		log.Printf("[search] WARN: Typesense health probe failed (%v) — "+
			"returning client anyway; will retry on first request", err)
	case !healthy:
		log.Printf("[search] WARN: Typesense reported unhealthy status — "+
			"returning client anyway; may be a transient 429 / cold-start")
	default:
		log.Println("[search] Typesense connection established ✓")
	}

	return &Client{ts: ts}, nil
}

// TS returns the underlying *typesense.Client so handlers can call
// client.TS().Collection(name).Documents().Search(...) with the full
// generics-aware type returned by the SDK (CollectionInterface[map[string]any]).
func (c *Client) TS() *typesense.Client {
	return c.ts
}
