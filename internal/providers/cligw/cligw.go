// Package cligw implements the shared logic for CLI-gateway backends
// (opencode, kilo): model ecosystems whose inference is only reachable
// through the user's own locally installed CLI / headless server using the
// user's own login. Zeuf never reads credential files and never invents
// endpoints: discovery uses `<bin> models --verbose`, health uses
// `<bin> providers|auth list` plus an optional serve-API ping, and inference
// delegates one transcript turn at a time to `<bin> run --format json`.
// Delegation is the integration these backends explicitly support; Zeuf
// keeps the outer session, routing, fallback and UI.
package cligw

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"zeuf/internal/core"
	"zeuf/internal/providers"
)

// Backend describes one CLI gateway.
type Backend struct {
	// Binary is the CLI name, e.g. "opencode" or "kilo".
	Binary string
	// Provider is the Zeuf provider id, e.g. "opencode" or "kilo".
	Provider string
	// ServeURL optionally points at a running `<bin> serve` instance for
	// richer discovery (GET /provider). Empty disables the serve path.
	ServeURL string
	// Workdir roots delegated runs.
	Workdir string
	// Timeout bounds one inference turn.
	Timeout time.Duration
}

// Adapter is the shared gateway implementation.
type Adapter struct {
	be     Backend
	client *http.Client
}

// New builds the adapter.
func New(be Backend) *Adapter {
	if be.Timeout == 0 {
		be.Timeout = 5 * time.Minute
	}
	return &Adapter{be: be, client: &http.Client{Timeout: 15 * time.Second}}
}

// Name implements providers.Adapter.
func (a *Adapter) Name() string { return a.be.Provider }

// Delegated implements providers.Adapter.
func (a *Adapter) Delegated() bool { return true }

func (a *Adapter) bin(ctx context.Context, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, a.be.Binary, args...)
	if a.be.Workdir != "" {
		c.Dir = a.be.Workdir
	}
	return c
}

// verboseModel mirrors the subset of `<bin> models --verbose` output Zeuf needs.
type verboseModel struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	IsFreeFlag *bool  `json:"isFree"`
	Cost       struct {
		Input  float64 `json:"input"`
		Output float64 `json:"output"`
	} `json:"cost"`
	HasCost bool `json:"-"`
	Limit   struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
	Capabilities struct {
		Toolcall bool `json:"toolcall"`
	} `json:"capabilities"`
}

// UnmarshalJSON records whether a cost object was present, so "known zero
// cost" (free) is distinguishable from "no pricing exposed" (unknown).
func (v *verboseModel) UnmarshalJSON(data []byte) error {
	type raw verboseModel
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	*v = verboseModel(r)
	var probe struct {
		Cost *struct{} `json:"cost"`
	}
	if err := json.Unmarshal(data, &probe); err == nil {
		v.HasCost = probe.Cost != nil
	}
	return nil
}

// freeFacts derives affirmative free/paid facts. An explicit free flag
// always wins (routers report isFree:false with a placeholder zero cost
// because their price is set at routing time). Otherwise a known zero
// cost means free. Anything else is unknown (not free).
func freeFacts(vm verboseModel) (isFree, costKnown bool, in, out float64) {
	if vm.IsFreeFlag != nil {
		if *vm.IsFreeFlag {
			return true, true, vm.Cost.Input, vm.Cost.Output
		}
		return false, vm.HasCost, vm.Cost.Input, vm.Cost.Output
	}
	if vm.HasCost {
		if vm.Cost.Input == 0 && vm.Cost.Output == 0 {
			return true, true, 0, 0
		}
		return false, true, vm.Cost.Input, vm.Cost.Output
	}
	return false, false, 0, 0
}

// ParseVerbose parses `<bin> models --verbose` output: lines of
// "provider/model" each followed by a JSON object. Plain (non-verbose)
// output is also accepted (models with unknown capabilities).
func ParseVerbose(provider, out string) []core.ModelInfo {
	var models []core.ModelInfo
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	var pending string
	var blk strings.Builder
	depth := 0
	flush := func() {
		if pending == "" || blk.Len() == 0 {
			return
		}
		var vm verboseModel
		if err := json.Unmarshal([]byte(blk.String()), &vm); err == nil && vm.ID != "" {
			id := strings.TrimPrefix(pending, provider+"/")
			isFree, costKnown, costIn, costOut := freeFacts(vm)
			models = append(models, core.ModelInfo{
				ID: id, Provider: provider,
				DisplayName: nonEmpty(vm.Name, id),
				Caps: core.Capabilities{
					ContextLength: vm.Limit.Context, MaxOutput: vm.Limit.Output,
					SupportsTools: vm.Capabilities.Toolcall, SupportsStreaming: true,
				},
				Scores:       core.UnknownScores(),
				Availability: core.AvailAvailable,
				QuotaState:   "unknown",
				IsFree:       isFree,
				CostKnown:    costKnown,
				CostInput:    costIn,
				CostOutput:   costOut,
			})
		}
		pending = ""
		blk.Reset()
		depth = 0
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if depth > 0 || strings.HasPrefix(line, "{") {
			blk.WriteString(line + "\n")
			depth += strings.Count(line, "{") - strings.Count(line, "}")
			if depth <= 0 {
				flush()
			}
			continue
		}
		if isModelLine(line) {
			flush()
			pending = line
			continue
		}
	}
	flush()
	if len(models) == 0 {
		// Plain output fallback: one "provider/model" per line.
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if !isModelLine(line) {
				continue
			}
			id := strings.TrimPrefix(line, provider+"/")
			prov := provider
			if parts := strings.SplitN(line, "/", 2); len(parts) == 2 {
				prov, id = parts[0], parts[1]
			}
			models = append(models, core.ModelInfo{
				ID: id, Provider: prov, DisplayName: id,
				Scores: core.UnknownScores(), Availability: core.AvailAvailable, QuotaState: "unknown",
			})
		}
	}
	return models
}

func isModelLine(s string) bool {
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "}") || strings.HasPrefix(s, `"`) {
		return false
	}
	parts := strings.SplitN(s, "/", 2)
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.Contains(s, " ")
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ListModels implements providers.Adapter.
func (a *Adapter) ListModels(ctx context.Context) ([]core.ModelInfo, error) {
	if a.be.ServeURL != "" {
		if ms, err := a.listViaServe(ctx); err == nil && len(ms) > 0 {
			return ms, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := a.bin(ctx, "models", "--verbose").Output()
	if err != nil {
		// Fall back to plain listing.
		out2, err2 := a.bin(ctx, "models").Output()
		if err2 != nil {
			return nil, &core.ProviderError{Code: core.ErrNetwork, Provider: a.be.Provider, Message: fmt.Sprintf("%s models failed: %v", a.be.Binary, err)}
		}
		out = out2
	}
	models := ParseVerbose(a.be.Provider, string(out))
	if len(models) == 0 {
		return nil, &core.ProviderError{Code: core.ErrUnknown, Provider: a.be.Provider, Message: "no models discovered"}
	}
	return models, nil
}

func (a *Adapter) listViaServe(ctx context.Context) ([]core.ModelInfo, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.be.ServeURL, "/")+"/provider", nil)
	req.Header.Set("Accept", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var v struct {
		All []verboseModel `json:"all"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, err
	}
	var out []core.ModelInfo
	for _, vm := range v.All {
		if vm.ProviderID != a.be.Provider && a.be.Provider != "" {
			// Serve lists every configured provider; keep ours plus anything
			// explicitly routed through this gateway is out of scope.
			continue
		}
		isFree, costKnown, costIn, costOut := freeFacts(vm)
		out = append(out, core.ModelInfo{
			ID: vm.ID, Provider: a.be.Provider, DisplayName: nonEmpty(vm.Name, vm.ID),
			Caps:   core.Capabilities{ContextLength: vm.Limit.Context, MaxOutput: vm.Limit.Output, SupportsTools: vm.Capabilities.Toolcall, SupportsStreaming: true},
			Scores: core.UnknownScores(), Availability: core.AvailAvailable, QuotaState: "unknown",
			IsFree: isFree, CostKnown: costKnown, CostInput: costIn, CostOutput: costOut,
		})
	}
	return out, nil
}

// Health implements providers.Adapter without consuming quota.
func (a *Adapter) Health(ctx context.Context) (providers.Health, error) {
	start := time.Now()
	if _, err := exec.LookPath(a.be.Binary); err != nil {
		return providers.Health{OK: false, Message: a.be.Binary + " not installed", Checked: time.Now()}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	listCmd := []string{"providers", "list"}
	if a.be.Binary == "kilo" {
		listCmd = []string{"auth", "list"}
	}
	c := a.bin(ctx, listCmd...)
	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf
	if err := c.Run(); err != nil {
		return providers.Health{OK: false, Message: "auth check failed: " + firstLine(core.Redact(buf.String())), Checked: time.Now()}, nil
	}
	if a.be.ServeURL != "" {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.be.ServeURL, "/")+"/config", nil)
		if resp, err := a.client.Do(req); err == nil {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
			resp.Body.Close()
		}
	}
	return providers.Health{OK: true, Message: "ok", Latency: time.Since(start), Checked: time.Now()}, nil
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// runTokens mirrors step-finish token accounting.
type runTokens struct {
	Total     int64 `json:"total"`
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
}

// runToolState mirrors a tool_use part state.
type runToolState struct {
	Status   string          `json:"status"`
	Input    json.RawMessage `json:"input"`
	Output   string          `json:"output"`
	Title    string          `json:"title"`
	Metadata struct {
		Exit int `json:"exit"`
	} `json:"metadata"`
}

// runEvent mirrors the `<bin> run --format json` JSONL envelope.
type runEvent struct {
	Type      string     `json:"type"`
	SessionID string     `json:"sessionID"`
	Text      string     `json:"text"`
	Tokens    *runTokens `json:"tokens"`
	Part      struct {
		Type   string       `json:"type"`
		Text   string       `json:"text"`
		Tool   string       `json:"tool"`
		CallID string       `json:"callID"`
		Tokens *runTokens   `json:"tokens"`
		State  runToolState `json:"state"`
	} `json:"part"`
	Error *struct {
		Name string `json:"name"`
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	} `json:"error"`
}

// runParsed is one decoded JSONL line.
type runParsed struct {
	textDelta      string
	reasoningDelta string
	usage          *core.Usage
	tool           *runToolProgress
	perr           *core.ProviderError
}

// runToolProgress describes one delegated server-side tool completion.
// Only completed uses are surfaced (running states would leave orphaned
// spinners); the agent loop never executes these — they already ran.
type runToolProgress struct {
	Name    string
	Preview string
	Ok      bool
}

// parseRunEvent decodes one `<bin> run --format json` line: text deltas,
// reasoning deltas (with --thinking), step-finish usage, completed
// delegated tool uses, and terminal errors.
func parseRunEvent(line string) runParsed {
	var out runParsed
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "{") {
		return out
	}
	var ev runEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return out
	}
	if ev.Type == "error" {
		msg := "backend error"
		if ev.Error != nil && ev.Error.Data.Message != "" {
			msg = ev.Error.Data.Message
		}
		out.perr = &core.ProviderError{Code: core.ClassifyMessage(msg), Message: msg}
		return out
	}
	switch ev.Type {
	case "text":
		if ev.Text != "" {
			out.textDelta = ev.Text
		} else if ev.Part.Text != "" {
			out.textDelta = ev.Part.Text
		}
	case "reasoning":
		if ev.Text != "" {
			out.reasoningDelta = ev.Text
		} else if ev.Part.Text != "" {
			out.reasoningDelta = ev.Part.Text
		}
	case "tool_use":
		if ev.Part.State.Status == "completed" {
			preview := ev.Part.State.Title
			if preview == "" {
				preview = firstLine(ev.Part.State.Output)
			}
			out.tool = &runToolProgress{Name: ev.Part.Tool, Preview: preview, Ok: ev.Part.State.Metadata.Exit == 0}
		}
	case "step_finish":
		tok := ev.Tokens
		if tok == nil {
			tok = ev.Part.Tokens
		}
		if tok != nil {
			out.usage = &core.Usage{Input: tok.Input, Output: tok.Output, Reasoning: tok.Reasoning}
		}
	}
	return out
}

// ParseRunLine folds one JSONL event's text into the accumulator,
// returning a terminal *core.ProviderError when the line is an error
// event. Reasoning, usage, and tool progress use parseRunEvent.
func ParseRunLine(line string, text *strings.Builder) *core.ProviderError {
	p := parseRunEvent(line)
	if p.textDelta != "" {
		text.WriteString(p.textDelta)
	}
	return p.perr
}

// promptFor renders a ChatRequest as the single transcript prompt the
// gateway consumes. Full history is included so model switches continue
// the same Zeuf session.
func promptFor(workdir string, req core.ChatRequest) string {
	var b strings.Builder
	for _, m := range req.Messages {
		switch m.Role {
		case core.RoleSystem:
			b.WriteString("System instructions:\n" + m.Content + "\n\n")
		case core.RoleUser:
			b.WriteString("User:\n" + m.Content + "\n\n")
		case core.RoleAssistant:
			if m.Content != "" {
				b.WriteString("Assistant:\n" + m.Content + "\n\n")
			}
			for _, tc := range m.ToolCalls {
				b.WriteString("Assistant used tool `" + tc.Name + "` (" + tc.Arguments + ")\n\n")
			}
		case core.RoleTool:
			b.WriteString("Tool result (" + m.Name + "):\n" + m.Content + "\n\n")
		}
	}
	b.WriteString(providers.ActionDirective(workdir) + "\n\n")
	b.WriteString("Reply to the latest user message above.")
	return b.String()
}

// Chat implements providers.Adapter via one delegated `run` turn.
func (a *Adapter) Chat(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, a.be.Timeout)
	defer cancel()
	model := a.be.Provider + "/" + req.Model
	if strings.Contains(req.Model, "/") {
		model = req.Model
	}
	c := a.bin(ctx, "run", "--format", "json", "--thinking", "-m", model)
	// The transcript travels on stdin, never argv: prompts carry session
	// content (visible to local users via ps) and can exceed ARG_MAX.
	c.Stdin = strings.NewReader(promptFor(a.be.Workdir, req))
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, &core.ProviderError{Code: core.ErrUnknown, Provider: a.be.Provider, Message: err.Error()}
	}
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err := c.Start(); err != nil {
		return nil, &core.ProviderError{Code: core.ErrNetwork, Provider: a.be.Provider, Message: err.Error()}
	}
	var text strings.Builder
	var usage core.Usage
	var perr *core.ProviderError
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	for sc.Scan() {
		p := parseRunEvent(sc.Text())
		if p.textDelta != "" {
			text.WriteString(p.textDelta)
		}
		if p.usage != nil {
			usage.Input += p.usage.Input
			usage.Output += p.usage.Output
			usage.Reasoning += p.usage.Reasoning
		}
		if p.perr != nil {
			p.perr.Provider = a.be.Provider
			p.perr.Model = req.Model
			perr = p.perr
		}
	}
	werr := c.Wait()
	if perr != nil {
		return nil, perr
	}
	if stderr.Len() > 0 && text.Len() == 0 {
		msg := firstLine(stderr.String())
		if werr != nil || msg != "" {
			return nil, &core.ProviderError{Code: core.ClassifyMessage(msg + " " + werrString(werr)), Provider: a.be.Provider, Model: req.Model, Message: nonEmpty(msg, "backend run failed")}
		}
	}
	if werr != nil && text.Len() == 0 {
		return nil, &core.ProviderError{Code: core.ClassifyMessage(werrString(werr)), Provider: a.be.Provider, Model: req.Model, Message: "backend run failed: " + werrString(werr)}
	}
	return &core.ChatResponse{Content: text.String(), Model: req.Model, Provider: a.be.Provider, Usage: usage}, nil
}

// Stream implements providers.Adapter by streaming `run` JSONL text parts.
func (a *Adapter) Stream(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error) {
	ctx, cancel := context.WithTimeout(ctx, a.be.Timeout)
	model := a.be.Provider + "/" + req.Model
	if strings.Contains(req.Model, "/") {
		model = req.Model
	}
	c := a.bin(ctx, "run", "--format", "json", "--thinking", "-m", model)
	// Transcript on stdin, never argv (see Chat).
	c.Stdin = strings.NewReader(promptFor(a.be.Workdir, req))
	stdout, err := c.StdoutPipe()
	if err != nil {
		cancel()
		return nil, &core.ProviderError{Code: core.ErrUnknown, Provider: a.be.Provider, Message: err.Error()}
	}
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err := c.Start(); err != nil {
		cancel()
		return nil, &core.ProviderError{Code: core.ErrNetwork, Provider: a.be.Provider, Message: err.Error()}
	}
	ch := make(chan core.StreamEvent, 64)
	go func() {
		defer close(ch)
		defer cancel()
		var text, reasoning strings.Builder
		var usage core.Usage
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 4<<20), 4<<20)
		emit := func(ev core.StreamEvent) bool {
			select {
			case ch <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for sc.Scan() {
			p := parseRunEvent(sc.Text())
			if p.perr != nil {
				p.perr.Provider = a.be.Provider
				p.perr.Model = req.Model
				emit(core.StreamEvent{Type: core.EventError, Err: p.perr})
				return
			}
			if p.textDelta != "" {
				before := text.Len()
				text.WriteString(p.textDelta)
				full := text.String()
				if !emit(core.StreamEvent{Type: core.EventToken, Delta: full[before:]}) {
					return
				}
			}
			if p.reasoningDelta != "" {
				// Skip exact-prefix repeats (some backends re-emit
				// cumulative snapshots); otherwise stream the chunk.
				if cur := reasoning.String(); cur == "" || !strings.HasPrefix(p.reasoningDelta, cur) && !strings.HasPrefix(cur, p.reasoningDelta) {
					reasoning.WriteString(p.reasoningDelta)
					if !emit(core.StreamEvent{Type: core.EventReasoning, Delta: p.reasoningDelta}) {
						return
					}
				} else if strings.HasPrefix(p.reasoningDelta, cur) && len(p.reasoningDelta) > len(cur) {
					delta := p.reasoningDelta[len(cur):]
					reasoning.WriteString(delta)
					if !emit(core.StreamEvent{Type: core.EventReasoning, Delta: delta}) {
						return
					}
				}
			}
			if p.usage != nil {
				usage.Input += p.usage.Input
				usage.Output += p.usage.Output
				usage.Reasoning += p.usage.Reasoning
			}
			if p.tool != nil {
				if !emit(core.StreamEvent{Type: core.EventToolProgress, Tool: p.tool.Name, Delta: p.tool.Preview, Done: true, Ok: p.tool.Ok}) {
					return
				}
			}
		}
		if werr := c.Wait(); werr != nil && text.Len() == 0 {
			msg := firstLine(stderr.String())
			emit(core.StreamEvent{Type: core.EventError, Err: &core.ProviderError{
				Code:     core.ClassifyMessage(msg + " " + werrString(werr)),
				Message:  nonEmpty(msg, "backend run failed"),
				Provider: a.be.Provider, Model: req.Model,
			}})
			return
		}
		emit(core.StreamEvent{Type: core.EventDone, Usage: usage})
	}()
	return ch, nil
}

func werrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
