// Package opencode adapts the user's own OpenCode installation as a Zeuf
// model backend. It shells out to the documented `opencode` CLI
// (models discovery, auth list, run --format json) and optionally reads
// the local `opencode serve` HTTP API for discovery. It never reads
// credential files or invents endpoints; inference for these gateway
// models is delegated turn-by-turn while Zeuf keeps session, routing,
// fallback and UI.
package opencode

import (
	"context"
	"time"

	"zeuf/internal/core"
	"zeuf/internal/providers"
	"zeuf/internal/providers/cligw"
)

// Config tunes the adapter.
type Config struct {
	ServeURL string
	Workdir  string
	Timeout  time.Duration
}

// Adapter is the OpenCode backend.
type Adapter struct{ inner *cligw.Adapter }

// New builds the adapter.
func New(cfg Config) *Adapter {
	return &Adapter{inner: cligw.New(cligw.Backend{
		Binary: "opencode", Provider: "opencode",
		ServeURL: cfg.ServeURL, Workdir: cfg.Workdir, Timeout: cfg.Timeout,
	})}
}

// Name implements providers.Adapter.
func (a *Adapter) Name() string { return "opencode" }

// Delegated implements providers.Adapter.
func (a *Adapter) Delegated() bool { return true }

// ListModels implements providers.Adapter.
func (a *Adapter) ListModels(ctx context.Context) ([]core.ModelInfo, error) {
	return a.inner.ListModels(ctx)
}

// Chat implements providers.Adapter.
func (a *Adapter) Chat(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
	return a.inner.Chat(ctx, req)
}

// Stream implements providers.Adapter.
func (a *Adapter) Stream(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error) {
	return a.inner.Stream(ctx, req)
}

// Health implements providers.Adapter.
func (a *Adapter) Health(ctx context.Context) (providers.Health, error) {
	return a.inner.Health(ctx)
}
