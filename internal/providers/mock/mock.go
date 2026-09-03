// Package mock provides scripted in-process backends for tests. No network,
// no quota, no credentials.
package mock

import (
	"context"
	"strings"
	"time"

	"zeuf/internal/core"
	"zeuf/internal/providers"
)

// Script is one canned turn: either a response or an error.
type Script struct {
	Resp *core.ChatResponse
	Err  error
}

// Adapter replays scripts in order (then repeats the last one).
type Adapter struct {
	name      string
	models    []core.ModelInfo
	scripts   []Script
	calls     int
	Streamed  [][]core.StreamEvent
	Requests  []core.ChatRequest
	Latencies []time.Duration
}

// New builds a mock backend.
func New(name string, models []core.ModelInfo, scripts []Script) *Adapter {
	return &Adapter{name: name, models: models, scripts: scripts}
}

// Name implements providers.Adapter.
func (a *Adapter) Name() string { return a.name }

// Delegated implements providers.Adapter.
func (a *Adapter) Delegated() bool { return false }

// Calls reports how many turns were executed.
func (a *Adapter) Calls() int { return a.calls }

// ListModels implements providers.Adapter.
func (a *Adapter) ListModels(ctx context.Context) ([]core.ModelInfo, error) {
	return append([]core.ModelInfo(nil), a.models...), nil
}

// Health implements providers.Adapter.
func (a *Adapter) Health(ctx context.Context) (providers.Health, error) {
	return providers.Health{OK: true, Message: "mock", Checked: time.Now(), Models: len(a.models)}, nil
}

func (a *Adapter) script() Script {
	if len(a.scripts) == 0 {
		return Script{Resp: &core.ChatResponse{Content: "mock reply"}}
	}
	if a.calls < len(a.scripts) {
		return a.scripts[a.calls]
	}
	return a.scripts[len(a.scripts)-1]
}

// Chat implements providers.Adapter.
func (a *Adapter) Chat(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
	a.Requests = append(a.Requests, req)
	s := a.script()
	a.calls++
	if s.Err != nil {
		return nil, s.Err
	}
	out := *s.Resp
	if out.Provider == "" {
		out.Provider = a.name
	}
	if out.Model == "" {
		out.Model = req.Model
	}
	return &out, nil
}

// Stream implements providers.Adapter by replaying the script as tokens.
func (a *Adapter) Stream(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error) {
	a.Requests = append(a.Requests, req)
	s := a.script()
	a.calls++
	ch := make(chan core.StreamEvent, 16)
	go func() {
		defer close(ch)
		if s.Err != nil {
			ch <- core.StreamEvent{Type: core.EventError, Err: s.Err}
			return
		}
		content := ""
		if s.Resp != nil {
			content = s.Resp.Content
		}
		for i, word := range strings.Split(content, " ") {
			delta := word
			if i > 0 {
				delta = " " + word
			}
			ch <- core.StreamEvent{Type: core.EventToken, Delta: delta}
		}
		if s.Resp != nil && len(s.Resp.ToolCalls) > 0 {
			ch <- core.StreamEvent{Type: core.EventTool, ToolCalls: s.Resp.ToolCalls}
		}
		var usage core.Usage
		if s.Resp != nil {
			usage = s.Resp.Usage
		}
		ch <- core.StreamEvent{Type: core.EventDone, Usage: usage}
	}()
	return ch, nil
}

// Model builds a ModelInfo for tests.
func Model(provider, id string, coding float64, ctxLen int, tools bool) core.ModelInfo {
	return core.ModelInfo{
		ID: id, Provider: provider, DisplayName: id,
		Caps:         core.Capabilities{ContextLength: ctxLen, SupportsTools: tools, SupportsStreaming: true},
		Scores:       core.Scores{Quality: 0.7, Coding: coding, Reasoning: 0.6, Latency: 0.5},
		Availability: core.AvailAvailable, QuotaState: "unknown",
	}
}
