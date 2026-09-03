// Package gemini adapts the user's own Gemini CLI installation as a Zeuf
// model backend. It shells out only to documented interfaces: headless
// prompts (`-p ... -o json|stream-json`), the `--yolo` approval flag, and
// the model IDs in the official model-selection docs. It never reads
// credential files (auth presence is an existence check plus env vars);
// inference for these gateway models is delegated turn-by-turn while Zeuf
// keeps session, routing, fallback and UI.
//
// Wire shapes come from https://geminicli.com/docs/cli/headless/:
// `-o json` returns {response, stats, error?}; `-o stream-json` emits
// JSONL init/message/tool_use/tool_result/error/result events. Exact
// nested field names are parsed defensively (several documented and
// observed variants) and covered by fixture tests.
package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// Adapter is the Gemini CLI backend.
type Adapter struct {
	bin     string
	workdir string
	timeout time.Duration
}

// New builds the adapter.
func New(cfg Config) *Adapter {
	bin := cfg.Binary
	if bin == "" {
		bin = "gemini"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	return &Adapter{bin: bin, workdir: cfg.Workdir, timeout: timeout}
}

// Name implements providers.Adapter.
func (a *Adapter) Name() string { return "gemini" }

// Delegated implements providers.Adapter.
func (a *Adapter) Delegated() bool { return true }

// oauthCreds is the stock Gemini CLI credential path. Zeuf only ever
// stats it (presence check); contents are never read.
func oauthCreds() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "oauth_creds.json")
}

// authed reports usable credentials: documented env vars or a prior
// `gemini` login (credential file presence only).
func authed() bool {
	for _, env := range []string{"GEMINI_API_KEY", "GOOGLE_GENAI_USE_VERTEXAI", "GOOGLE_GENAI_USE_GCA"} {
		if strings.TrimSpace(os.Getenv(env)) != "" {
			return true
		}
	}
	if st, err := os.Stat(oauthCreds()); err == nil && !st.IsDir() {
		return true
	}
	return false
}

// KnownModels returns the documented model IDs. Context is stated only
// where publicly documented (the 1M-token Flash/Pro generations);
// everything else stays honestly unknown. Models marked free are eligible
// for Google's $0 tier (Gemini CLI Google login / AI Studio free quota) —
// paid API keys and Vertex still meter, and remaining quota is never
// exposed, so QuotaState stays unknown. Models without free-tier evidence
// are listed but not marked free.
func KnownModels() []core.ModelInfo {
	const meg = 1048576
	mk := func(id, display string, ctx int, free bool) core.ModelInfo {
		return core.ModelInfo{
			ID: id, Provider: "gemini", DisplayName: display,
			Caps: core.Capabilities{
				ContextLength: ctx, SupportsTools: true, SupportsStreaming: true,
			},
			Scores: core.UnknownScores(), Availability: core.AvailUnknown,
			QuotaState: "unknown", IsFree: free, CostKnown: free,
		}
	}
	return []core.ModelInfo{
		mk("gemini-2.5-pro", "Gemini 2.5 Pro", meg, false),
		mk("gemini-2.5-flash", "Gemini 2.5 Flash", meg, true),
		mk("gemini-2.5-flash-lite", "Gemini 2.5 Flash-Lite", meg, true),
		mk("gemini-2.0-flash", "Gemini 2.0 Flash", meg, true),
		mk("gemini-3-pro-preview", "Gemini 3 Pro Preview", 0, false),
		mk("gemini-3-flash-preview", "Gemini 3 Flash Preview", 0, false),
		mk("gemini-3.1-pro", "Gemini 3.1 Pro", meg, false),
		mk("gemini-3.5-flash", "Gemini 3.5 Flash", meg, false),
		mk("gemini-3.5-flash-lite", "Gemini 3.5 Flash-Lite", 0, true),
		mk("gemini-3.6-flash", "Gemini 3.6 Flash", meg, true),
		withMaxOut(mk("gemini-3.7-flash", "Gemini 3.7 Flash", meg, false), 65536),
		withMaxOut(mk("gemini-3.8-flash", "Gemini 3.8 Flash", meg, false), 65536),
	}
}

// withMaxOut attaches a documented output limit. Used sparingly — only
// where Google publishes the figure (3.7/3.8 Flash: 65,536).
func withMaxOut(m core.ModelInfo, n int) core.ModelInfo {
	m.Caps.MaxOutput = n
	return m
}

// ListModels implements providers.Adapter. The CLI exposes no model
// enumeration, so the documented IDs are reported; availability reflects
// the auth check, never fabricated quota.
func (a *Adapter) ListModels(ctx context.Context) ([]core.ModelInfo, error) {
	if _, err := exec.LookPath(a.bin); err != nil {
		return nil, &core.ProviderError{Code: core.ErrNetwork, Provider: "gemini", Message: "gemini CLI not installed (run install.sh or `npm i -g @google/gemini-cli`)"}
	}
	ms := KnownModels()
	if authed() {
		for i := range ms {
			ms[i].Availability = core.AvailAvailable
		}
		return ms, nil
	}
	for i := range ms {
		ms[i].Availability = core.AvailAuthError
		ms[i].LastError = "no gemini auth: set GEMINI_API_KEY (free AI Studio key) — CLI OAuth login is end-of-life for individuals"
	}
	return ms, nil
}

// Health implements providers.Adapter without spending quota.
func (a *Adapter) Health(ctx context.Context) (providers.Health, error) {
	if _, err := exec.LookPath(a.bin); err != nil {
		return providers.Health{OK: false, Message: "gemini CLI not installed", Checked: time.Now()}, nil
	}
	if !authed() {
		return providers.Health{OK: false, Message: "no gemini auth: set GEMINI_API_KEY (free AI Studio key) — CLI OAuth login is end-of-life for individuals", Checked: time.Now()}, nil
	}
	return providers.Health{OK: true, Message: "ok", Checked: time.Now(), Models: len(KnownModels())}, nil
}

// modelFlag strips an optional "gemini/" FullID prefix for -m.
func modelFlag(model string) string {
	return strings.TrimPrefix(model, "gemini/")
}

func (a *Adapter) runArgs(model, format, prompt string) []string {
	return []string{"-p", prompt, "-m", modelFlag(model), "-o", format, "--yolo", "--skip-trust"}
}

// runCmd builds the headless invocation rooted at the workdir.
func (a *Adapter) runCmd(ctx context.Context, model, format, prompt string) *exec.Cmd {
	c := exec.CommandContext(ctx, a.bin, a.runArgs(model, format, prompt)...)
	if a.workdir != "" {
		c.Dir = a.workdir
	}
	return c
}

// classifyExit maps documented headless exit codes.
func classifyExit(code int, msg string) core.ErrorCode {
	switch code {
	case 42:
		return core.ErrUnsupported
	case 53:
		return core.ErrOverloaded
	default:
		return core.ClassifyMessage(msg)
	}
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
	var v map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		if werr != nil {
			return nil, exitErr(req.Model, werr, meaningfulStderr(stderr.String()))
		}
		return nil, &core.ProviderError{Code: core.ErrUnknown, Provider: "gemini", Model: req.Model, Message: "decode gemini response: " + err.Error()}
	}
	if em, ok := v["error"].(map[string]any); ok && len(em) > 0 {
		msg := jstr(em, "message", "text")
		if msg == "" {
			msg = "gemini request failed"
		}
		code := core.ClassifyMessage(msg)
		if werr != nil {
			if ec, ok := exitCode(werr); ok {
				code = classifyExit(ec, msg)
			}
		}
		return nil, &core.ProviderError{Code: code, Provider: "gemini", Model: req.Model, Message: msg}
	}
	var usage core.Usage
	if sm, ok := v["stats"].(map[string]any); ok {
		usage = statsUsage(sm)
	}
	if werr != nil {
		// Non-zero exit with a parsed body: surface when empty.
		if strings.TrimSpace(messageText(v)) == "" {
			return nil, exitErr(req.Model, werr, meaningfulStderr(stderr.String()))
		}
	}
	return &core.ChatResponse{Content: messageText(v), Model: req.Model, Provider: "gemini", Usage: usage}, nil
}

// Stream implements providers.Adapter over `-o stream-json` JSONL.
func (a *Adapter) Stream(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	c := a.runCmd(ctx, req.Model, "stream-json", promptFor(a.workdir, req))
	stdout, err := c.StdoutPipe()
	if err != nil {
		cancel()
		return nil, &core.ProviderError{Code: core.ErrUnknown, Provider: "gemini", Message: err.Error()}
	}
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err := c.Start(); err != nil {
		cancel()
		return nil, &core.ProviderError{Code: core.ErrNetwork, Provider: "gemini", Message: err.Error()}
	}
	ch := make(chan core.StreamEvent, 64)
	go func() {
		defer close(ch)
		defer cancel()
		var usage core.Usage
		var warnings []string
		var sawResult bool
		pending := map[string]string{} // tool id -> name
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
			case "message":
				if ev.text != "" {
					if !emit(core.StreamEvent{Type: core.EventToken, Delta: ev.text}) {
						return
					}
				}
			case "tool_use":
				if ev.tool != "" {
					id := ev.id
					if id == "" {
						id = "\x00" + ev.tool
					}
					pending[id] = ev.tool
					if !emit(core.StreamEvent{Type: core.EventToolProgress, Tool: ev.tool, Delta: ev.text, Done: false}) {
						return
					}
				}
			case "tool_result":
				name, ok := pending[ev.id]
				if !ok && ev.id != "" {
					// Fall back to FIFO match on bare names.
					for k, n := range pending {
						name, ok = n, true
						delete(pending, k)
						break
					}
				} else if ok {
					delete(pending, ev.id)
				}
				if ev.tool != "" {
					name = ev.tool
				}
				if name == "" {
					name = "tool"
				}
				if !emit(core.StreamEvent{Type: core.EventToolProgress, Tool: name, Delta: ev.text, Done: true, Ok: !ev.failed}) {
					return
				}
			case "result":
				sawResult = true
				usage.Input += ev.usage.Input
				usage.Output += ev.usage.Output
				usage.Reasoning += ev.usage.Reasoning
			case "error":
				if ev.text != "" {
					warnings = append(warnings, ev.text)
				}
			}
		}
		werr := c.Wait()
		if !sawResult {
			msg := strings.Join(warnings, "; ")
			if msg == "" {
				msg = meaningfulStderr(stderr.String())
			}
			if werr != nil || msg != "" {
				code := core.ClassifyMessage(msg + " " + werrString(werr))
				if werr != nil {
					if ec, ok := exitCode(werr); ok {
						code = classifyExit(ec, msg)
					}
				}
				emit(core.StreamEvent{Type: core.EventError, Err: &core.ProviderError{
					Code: code, Message: nonEmpty(msg, "gemini run failed"),
					Provider: "gemini", Model: req.Model,
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
	kind   string // message|tool_use|tool_result|error|result|init|""
	text   string
	id     string
	tool   string
	failed bool
	usage  core.Usage
}

// parseStreamLine decodes one stream-json event defensively: only the
// envelope type names are documented, so payload keys are probed across
// the variants the CLI has used.
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
	typ, _ := v["type"].(string)
	payload := v
	isTop := true
	if p, ok := v["payload"].(map[string]any); ok {
		payload = p
		isTop = false
	}
	// Some builds nest under data/event.
	if isTop {
		for _, k := range []string{"data", "event"} {
			if p, ok := v[k].(map[string]any); ok {
				if _, has := p["type"]; has {
					if t, ok := p["type"].(string); ok && typ == "" {
						typ = t
					}
					payload = p
				}
			}
		}
	}
	out.kind = typ
	switch typ {
	case "message":
		out.text = messageText(payload)
	case "tool_use":
		out.id = jstr(payload, "callId", "call_id", "toolCallId", "tool_call_id", "id")
		out.tool = jstr(payload, "name", "tool", "function")
		out.text = jstr(payload, "title", "description", "summary")
		if out.text == "" {
			if args := jstr(payload, "args", "arguments", "input", "parameters"); args != "" {
				out.text = args
			}
		}
	case "tool_result":
		out.id = jstr(payload, "callId", "call_id", "toolCallId", "tool_call_id", "id")
		out.tool = jstr(payload, "name", "tool", "function")
		out.text = jstr(payload, "output", "result", "text", "content", "message")
		out.failed = jbool(payload, "isError", "is_error", "error", "failed")
		if s, ok := payload["status"].(string); ok {
			ls := strings.ToLower(s)
			if strings.Contains(ls, "fail") || strings.Contains(ls, "error") {
				out.failed = true
			}
		}
	case "error":
		out.text = jstr(payload, "message", "text", "error")
		if out.text == "" {
			if em, ok := payload["error"]; ok {
				out.text = fmt.Sprint(em)
			}
		}
	case "result":
		if sm, ok := payload["stats"].(map[string]any); ok {
			out.usage = statsUsage(sm)
		}
		if mm, ok := payload["models"].([]any); ok {
			for _, item := range mm {
				if im, ok := item.(map[string]any); ok {
					u := statsUsage(im)
					out.usage.Input += u.Input
					out.usage.Output += u.Output
					out.usage.Reasoning += u.Reasoning
				}
			}
		}
	}
	return out
}

// messageText extracts assistant text across payload shapes.
func messageText(v map[string]any) string {
	if s := jstr(v, "response", "text", "content", "delta", "message", "output", "result"); s != "" {
		return s
	}
	if arr, ok := v["content"].([]any); ok {
		var parts []string
		for _, item := range arr {
			if im, ok := item.(map[string]any); ok {
				if s := jstr(im, "text", "content"); s != "" {
					parts = append(parts, s)
				}
			} else if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "")
		}
	}
	if m, ok := v["message"].(map[string]any); ok {
		if s := messageText(m); s != "" {
			return s
		}
	}
	return ""
}

// statsUsage folds token counters across key variants.
func statsUsage(m map[string]any) core.Usage {
	in, _ := jnum(m, "input_tokens", "inputTokens", "prompt_tokens", "promptTokens", "input")
	out, _ := jnum(m, "output_tokens", "outputTokens", "completion_tokens", "completionTokens", "output")
	total, hasTotal := jnum(m, "total_tokens", "totalTokens", "total")
	if !hasTotal {
		if tm, ok := m["tokens"].(map[string]any); ok {
			sub := statsUsage(tm)
			if in == 0 {
				in = sub.Input
			}
			if out == 0 {
				out = sub.Output
			}
		}
	}
	_ = total
	return core.Usage{Input: in, Output: out}
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

func jbool(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if b, ok := m[k].(bool); ok && b {
			return true
		}
	}
	return false
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
// stderr line (e.g. the real Error authenticating: … line).
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

func exitCode(err error) (int, bool) {
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), true
	}
	return 0, false
}

func exitErr(model string, werr error, msg string) error {
	code := core.ClassifyMessage(msg + " " + werrString(werr))
	if ec, ok := exitCode(werr); ok {
		code = classifyExit(ec, msg)
	}
	return &core.ProviderError{Code: code, Provider: "gemini", Model: model, Message: nonEmpty(msg, "gemini run failed: "+werrString(werr))}
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
