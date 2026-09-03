// Package providers defines the Adapter interface every model backend
// implements, plus the shared health type. Provider-specific logic lives
// only in the subpackages (direct, opencode, kilo, mock).
package providers

import (
	"context"
	"strings"
	"time"

	"zeuf/internal/core"
)

// ActionDirective frames delegated turns so gateway agents act in the
// workspace instead of dictating code. The agent still decides everything
// from intent; this only states the standing job — never keyword rules,
// never per-verb branches.
func ActionDirective(workdir string) string {
	var b strings.Builder
	b.WriteString("You are Zeuf, acting in the user's terminal")
	if wd := strings.TrimSpace(workdir); wd != "" {
		b.WriteString(" (working directory: " + wd + ")")
	}
	b.WriteString(`. You have your full toolset available there.
ACT on the workspace with your tools: inspect the repository, implement changes with file operations, run builds and tests, fix failures, and verify the result. Do the task yourself — do not merely print code for the user to apply.
Ordinary development work proceeds directly; ask before anything destructive.
When finished, summarize briefly: what changed (paths), and how it was verified.`)
	return b.String()
}

// Health is a point-in-time liveness report. It never exposes credentials.
type Health struct {
	OK      bool          `json:"ok"`
	Message string        `json:"message"`
	Latency time.Duration `json:"-"`
	Models  int           `json:"models,omitempty"`
	Checked time.Time     `json:"checked"`
}

// Adapter normalizes one model ecosystem behind the interface Forge's
// router and agent use. Only implement what the backend truly supports;
// return a *core.ProviderError with ErrUnsupported otherwise.
type Adapter interface {
	// Name is the backend id: "direct", "opencode", "kilo", "mock".
	Name() string
	// Delegated reports whether inference runs inside the backend's own
	// agent (true for gateway CLIs that expose no raw model API) or
	// natively in Zeuf's loop (direct HTTP providers).
	Delegated() bool
	// ListModels discovers models without consuming quota.
	ListModels(ctx context.Context) ([]core.ModelInfo, error)
	// Chat performs one non-streaming turn.
	Chat(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error)
	// Stream performs one streaming turn.
	Stream(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error)
	// Health checks backend reachability without consuming quota.
	Health(ctx context.Context) (Health, error)
}
