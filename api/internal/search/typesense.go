// Package search wraps the Typesense Go client and exposes a clean interface
// used by the HTTP handlers. It is intentionally thin — all search logic lives
// in the handler layer so that parameters flow transparently.
package search

import (
	"context"
	"fmt"
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

	// Verify connectivity — Health() performs a GET /health.
	// Signature: Health(ctx context.Context, timeout time.Duration) (bool, error)
	healthy, err := ts.Health(context.Background(), 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("typesense health check: %w", err)
	}
	if !healthy {
		return nil, fmt.Errorf("typesense reported unhealthy status")
	}

	log.Println("[search] Typesense connection established ✓")
	return &Client{ts: ts}, nil
}

// TS returns the underlying *typesense.Client so handlers can call
// client.TS().Collection(name).Documents().Search(...) with the full
// generics-aware type returned by the SDK (CollectionInterface[map[string]any]).
func (c *Client) TS() *typesense.Client {
	return c.ts
}
