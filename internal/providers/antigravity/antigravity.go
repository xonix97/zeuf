// Package antigravity adapts the user's own Antigravity CLI (`agy`) as a
// Zeuf model backend. It shells out only to documented interfaces:
// `agy models` for discovery and headless turns
// (`-p … --output-format json|stream-json`) with the documented
// `--dangerously-skip-permissions` flag so delegated coding turns can act
// in the user's workdir. Model slugs (including -high/-medium/-low effort
// variants) come straight from `agy models` — never hardcoded. Zeuf keeps
// session, routing, fallback and UI; inference is delegated turn-by-turn.
//
// Wire shapes come from https://antigravity.google/docs/cli/headless/:
// -o json gives {conversation_id, status, response, usage{input_tokens,
// output_tokens, thinking_tokens, ...}}; stream-json emits init /
// step_update (agent_response text deltas, tool ACTIVE/DONE with
// tool_name/tool_info/output) / result events. Thinking happens
// server-side — only token counts surface, never thinking text.
package antigravity

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"zeuf/internal/core"
	"zeuf/internal/providers"
)

// Config tunes the adapter.
type Config struct {
	Binary  string
	Workdir string
	Timeout time.Duration
}

// Adapter is the Antigravity CLI backend.
type Adapter struct {
	bin     string
	workdir string
	timeout time.Duration
}

// New builds the adapter.
func New(cfg Config) *Adapter {
	bin := cfg.Binary
	if bin == "" {
		bin = "agy"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	return &Adapter{bin: bin, workdir: cfg.Workdir, timeout: timeout}
}

// Name implements providers.Adapter.
func (a *Adapter) Name() string { return "agy" }

// Delegated implements providers.Adapter.
func (a *Adapter) Delegated() bool { return true }

// ListModels implements providers.Adapter via `agy models`. Slugs and
// display names are parsed live; context/quota/scores stay honestly
// unknown (the listing carries no metadata, and credit state is not
// observable).
func (a *Adapter) ListModels(ctx context.Context) ([]core.ModelInfo, error) {
	if _, err := exec.LookPath(a.bin); err != nil {
		return nil, &core.ProviderError{Code: core.ErrNetwork, Provider: "agy", Message: "agy CLI not installed (run install.sh)"}
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, a.bin, "models")
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return nil, &core.ProviderError{Code: core.ClassifyMessage(firstLine(stderr.String()) + " " + werrString(err)), Provider: "agy", Message: nonEmpty(firstLine(stderr.String()), "agy models failed")}
	}
	var out []core.ModelInfo
	for _, ln := range strings.Split(stdout.String(), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "Fetching") {
			continue
		}
		fields := strings.Fields(ln)
		if len(fields) == 0 {
			continue
		}
		id := fields[0]
		display := strings.TrimSpace(strings.TrimPrefix(ln, id))
		if display == "" {
			display = id
		}
		out = append(out, core.ModelInfo{
			ID: id, Provider: "agy", DisplayName: display,
			Caps: core.Capabilities{
				SupportsTools: true, SupportsStreaming: true,
			},
			Scores: core.UnknownScores(), Availability: core.AvailAvailable,
			QuotaState: "unknown",
		})
	}
	if len(out) == 0 {
		return nil, &core.ProviderError{Code: core.ErrUnknown, Provider: "agy", Message: "agy models listed nothing"}
	}
	return out, nil
}

// Health implements providers.Adapter without spending quota: the models
// listing itself is the check.
func (a *Adapter) Health(ctx context.Context) (providers.Health, error) {
	if _, err := exec.LookPath(a.bin); err != nil {
		return providers.Health{OK: false, Message: "agy CLI not installed", Checked: time.Now()}, nil
	}
	ms, err := a.ListModels(ctx)
	if err != nil {
		return providers.Health{OK: false, Message: "agy models failed: " + firstLine(err.Error()), Checked: time.Now()}, nil
	}
	return providers.Health{OK: true, Message: "ok", Checked: time.Now(), Models: len(ms)}, nil
}

// modelFlag strips an optional "agy/" FullID prefix for --model.
func modelFlag(model string) string {
	return strings.TrimPrefix(model, "agy/")
}

func (a *Adapter) runArgs(model, format, prompt string) []string {
	return []string{"-p", prompt, "--model", modelFlag(model), "--output-format", format, "--dangerously-skip-permissions"}
}

func (a *Adapter) runCmd(ctx context.Context, model, format, prompt string) *exec.Cmd {
	c := exec.CommandContext(ctx, a.bin, a.runArgs(model, format, prompt)...)
	if a.workdir != "" {
		c.Dir = a.workdir
	}
	return c
}

// usageOf folds the documented token counters.
func usageOf(m map[string]any) core.Usage {
	in, _ := jnum(m, "input_tokens", "inputTokens", "prompt_tokens", "input")
	out, _ := jnum(m, "output_tokens", "outputTokens", "completion_tokens", "output")
	think, _ := jnum(m, "thinking_tokens", "thinkingTokens", "reasoning_tokens")
	return core.Usage{Input: in, Output: out, Reasoning: think}
}

// Chat implements providers.Adapter via one delegated headless turn.
func (a *Adapter) Chat(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	c := a.runCmd(ctx, req.Model, "json", promptFor(a.workdir, req))
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	werr := c.Run()
	raw := bytes.TrimSpace(stdout.Bytes())
	if len(raw) > 0 && raw[0] == '{' {
		var v map[string]any
		if err := json.Unmarshal(raw, &v); err == nil {
			if status, _ := v["status"].(string); status != "" && status != "SUCCESS" {
				msg := jstr(v, "error", "message")
				if msg == "" {
					msg = "agy run status: " + status
				}
				return nil, &core.ProviderError{Code: core.ClassifyMessage(msg), Provider: "agy", Model: req.Model, Message: msg}
			}
			var usage core.Usage
			if um, ok := v["usage"].(map[string]any); ok {
				usage = usageOf(um)
			}
			if werr != nil && strings.TrimSpace(messageText(v)) == "" {
				return nil, exitErr(req.Model, werr, meaningfulStderr(stderr.String()))
			}
			return &core.ChatResponse{Content: messageText(v), Model: req.Model, Provider: "agy", Usage: usage}, nil
		}
	}
	if werr != nil {
		return nil, exitErr(req.Model, werr, meaningfulStderr(stderr.String()))
	}
	// Zero exit, unparsed body: treat trimmed stdout as the answer.
	return &core.ChatResponse{Content: strings.TrimSpace(stdout.String()), Model: req.Model, Provider: "agy"}, nil
}

// Stream implements providers.Adapter over stream-json NDJSON.
func (a *Adapter) Stream(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	c := a.runCmd(ctx, req.Model, "stream-json", promptFor(a.workdir, req))
	stdout, err := c.StdoutPipe()
	if err != nil {
		cancel()
		return nil, &core.ProviderError{Code: core.ErrUnknown, Provider: "agy", Message: err.Error()}
	}
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err := c.Start(); err != nil {
		cancel()
		return nil, &core.ProviderError{Code: core.ErrNetwork, Provider: "agy", Message: err.Error()}
	}
	ch := make(chan core.StreamEvent, 64)
	go func() {
		defer close(ch)
		defer cancel()
		var usage core.Usage
		var sawResult bool
		var problems []string
		emit := func(ev core.StreamEvent) bool {
			select {
			case ch <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 4<<20), 4<<20)
		for sc.Scan() {
			ev := parseStreamLine(sc.Text())
			switch ev.kind {
			case "token":
				if !emit(core.StreamEvent{Type: core.EventToken, Delta: ev.text}) {
					return
				}
			case "tool_start":
				if !emit(core.StreamEvent{Type: core.EventToolProgress, Tool: ev.tool, Delta: ev.text, Done: false}) {
					return
				}
			case "tool_end":
				if !emit(core.StreamEvent{Type: core.EventToolProgress, Tool: ev.tool, Delta: ev.text, Done: true, Ok: !ev.failed}) {
					return
				}
			case "result":
				sawResult = true
				usage = ev.usage
			case "error":
				if ev.text != "" {
					problems = append(problems, ev.text)
				}
			}
		}
		werr := c.Wait()
		if !sawResult {
			msg := strings.Join(problems, "; ")
			if msg == "" {
				msg = meaningfulStderr(stderr.String())
			}
			if werr != nil || msg != "" {
				emit(core.StreamEvent{Type: core.EventError, Err: &core.ProviderError{
					Code:     core.ClassifyMessage(msg + " " + werrString(werr)),
					Message:  nonEmpty(msg, "agy run failed"),
					Provider: "agy", Model: req.Model,
				}})
				return
			}
		}
		emit(core.StreamEvent{Type: core.EventDone, Usage: usage})
	}()
	return ch, nil
}

// streamEvt is one decoded stream-json line.
type streamEvt struct {
	kind   string // token|tool_start|tool_end|result|error|""
	text   string
	tool   string
	failed bool
	usage  core.Usage
}

// parseStreamLine decodes one NDJSON event: step_update agent_response
// deltas stream as tokens, tool ACTIVE/DONE pairs as progress rows, and
// the terminal result carries authoritative usage.
func parseStreamLine(line string) streamEvt {
	var out streamEvt
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "{") {
		return out
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(line), &v); err != nil {
		return out
	}
	typ, _ := v["event"].(string)
	switch typ {
	case "init":
		return out
	case "step_update":
		su, _ := v["step_update"].(map[string]any)
		if su == nil {
			return out
		}
		stype, _ := su["step_type"].(string)
		state, _ := su["state"].(string)
		switch stype {
		case "agent_response":
			if d := jstr(su, "text_delta", "text", "delta", "content"); d != "" {
				out.kind, out.text = "token", d
			}
			// Usage snapshots may ride along; the terminal result wins.
		case "tool":
			name := jstr(su, "tool_name", "name", "tool")
			if name == "" {
				return out
			}
			out.tool = name
			info, _ := su["tool_info"].(map[string]any)
			preview := ""
			if info != nil {
				preview = toolPreview(info)
				if es, ok := info["error"]; ok && es != nil {
					if s, ok := es.(string); ok && s != "" {
						preview = s
						out.failed = true
					} else if b, ok := es.(bool); ok && b {
						out.failed = true
					}
				}
				if s, ok := info["status"].(string); ok {
					ls := strings.ToLower(s)
					if strings.Contains(ls, "fail") || strings.Contains(ls, "error") {
						out.failed = true
					}
				}
			}
			if preview == "" {
				preview = name
			}
			out.text = preview
			if strings.ToUpper(state) == "DONE" {
				out.kind = "tool_end"
			} else {
				out.kind = "tool_start"
			}
		}
	case "result":
		r, _ := v["result"].(map[string]any)
		if r == nil {
			// Tolerate a flat result envelope.
			r = v
		}
		if status, _ := r["status"].(string); status != "" && status != "SUCCESS" {
			out.kind, out.text = "error", jstr(r, "error", "message")
			if out.text == "" {
				out.text = "agy result status: " + status
			}
			return out
		}
		out.kind = "result"
		if um, ok := r["usage"].(map[string]any); ok {
			out.usage = usageOf(um)
		}
	case "error":
		out.kind, out.text = "error", jstr(v, "message", "text", "error")
		if out.text == "" {
			out.text = strings.TrimSpace(line)
		}
	}
	return out
}

// toolPreview condenses tool parameters/output to one UI line.
func toolPreview(info map[string]any) string {
	if s := jstr(info, "output", "result", "summary", "title"); s != "" {
		return firstPreviewLine(s)
	}
	params, _ := info["parameters"].(map[string]any)
	if params != nil {
		if s := jstr(params, "CommandLine", "command", "cmd", "path", "file", "pattern", "query", "url"); s != "" {
			return s
		}
		if raw, err := json.Marshal(params); err == nil && len(raw) < 200 {
			return string(raw)
		}
	}
	return ""
}

func firstPreviewLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if ln = strings.TrimSpace(strings.TrimRight(ln, "\r")); ln != "" {
			if len([]rune(ln)) > 140 {
				ln = string([]rune(ln)[:140]) + "…"
			}
			return ln
		}
	}
	return ""
}

// messageText extracts assistant text across -o json response shapes.
func messageText(v map[string]any) string {
	if s := jstr(v, "response", "text", "content", "message", "output", "result"); s != "" {
		return s
	}
	if m, ok := v["message"].(map[string]any); ok {
		return messageText(m)
	}
	return ""
}

func jstr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func jnum(m map[string]any, keys ...string) (int64, bool) {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return int64(v), true
		case int64:
			return v, true
		case int:
			return int64(v), true
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// bannerPrefixes are CLI notices, never the actual failure.
var bannerPrefixes = []string{
	"YOLO mode is enabled",
	"Approval mode overridden",
}

// meaningfulStderr skips banner notices and returns the first substantive
// stderr line.
func meaningfulStderr(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		banner := false
		for _, p := range bannerPrefixes {
			if strings.HasPrefix(ln, p) {
				banner = true
				break
			}
		}
		if !banner {
			return ln
		}
	}
	return ""
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func werrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func exitErr(model string, werr error, msg string) error {
	code := core.ClassifyMessage(msg + " " + werrString(werr))
	return &core.ProviderError{Code: code, Provider: "agy", Model: model, Message: nonEmpty(msg, "agy run failed: "+werrString(werr))}
}

// promptFor renders a ChatRequest as the single transcript prompt the
// gateway consumes (shared shape with the other CLI gateways).
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
