// Package direct implements the native OpenAI-compatible HTTP adapter.
// Zeuf owns the full agent loop here: chat completions with tool calling
// and SSE streaming go straight to the configured provider. It works with
// OpenAI, OpenRouter, Ollama, LM Studio, and any OpenAI-compatible gateway.
// The API key is read from the environment (never from project files) and
// never logged.
package direct

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"zeuf/internal/auth"
	"zeuf/internal/core"
	"zeuf/internal/providers"
)

// Config describes one direct endpoint.
type Config struct {
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	APIKeyEnv  string `json:"api_key_env"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

// Adapter is a native OpenAI-compatible backend.
type Adapter struct {
	cfg    Config
	client *http.Client
}

// New builds the adapter.
func New(cfg Config) *Adapter {
	if cfg.Name == "" {
		cfg.Name = "direct"
	}
	timeout := 120 * time.Second
	if cfg.TimeoutSec > 0 {
		timeout = time.Duration(cfg.TimeoutSec) * time.Second
	}
	return &Adapter{cfg: cfg, client: &http.Client{Timeout: timeout}}
}

// Name implements providers.Adapter.
func (a *Adapter) Name() string { return "direct:" + a.cfg.Name }

// Delegated implements providers.Adapter (Zeuf owns the loop).
func (a *Adapter) Delegated() bool { return false }

func (a *Adapter) key() (string, error) {
	if a.cfg.APIKeyEnv != "" {
		if k := strings.TrimSpace(os.Getenv(a.cfg.APIKeyEnv)); k != "" {
			return k, nil
		}
	}
	// Fall back to the Zeuf auth store (populated by /connect).
	store, _ := auth.Open()
	if store != nil {
		if k, err := store.Get(auth.ServiceDirect, a.cfg.Name); err == nil && strings.TrimSpace(k) != "" {
			return strings.TrimSpace(k), nil
		}
	}
	want := a.cfg.APIKeyEnv
	if want == "" {
		want = "a stored credential (run /connect or `zeuf connect`)"
	}
	return "", &core.ProviderError{Code: core.ErrAuth, Provider: a.Name(), Message: fmt.Sprintf("no API key: %s is not set and no stored credential exists", want)}
}

// keyless reports whether the endpoint declares no credential at all
// (empty APIKeyEnv and nothing in the auth store), as with local servers
// like Ollama. Callers then omit the Authorization header instead of
// failing: a server that still wants a key answers 401, honestly mapped.
func (a *Adapter) keyless() bool {
	if strings.TrimSpace(a.cfg.APIKeyEnv) != "" {
		return false
	}
	store, _ := auth.Open()
	if store != nil {
		if k, err := store.Get(auth.ServiceDirect, a.cfg.Name); err == nil && strings.TrimSpace(k) != "" {
			return false
		}
	}
	return true
}

// isLoopbackURL reports loopback bases (localhost, 127/8, ::1): traffic
// never leaves the machine, so nothing can be metered — models there are
// affirmatively free, not "unknown".
func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "::1" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// ListModels implements providers.Adapter via GET /models (best effort:
// the chat API always works even if listing is unsupported).
func (a *Adapter) ListModels(ctx context.Context) ([]core.ModelInfo, error) {
	key, err := a.key()
	if err != nil && !a.keyless() {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.cfg.BaseURL, "/")+"/models", nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, &core.ProviderError{Code: core.ErrNetwork, Provider: a.Name(), Message: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return nil, &core.ProviderError{Code: core.ClassifyHTTPStatus(resp.StatusCode, string(body)), Provider: a.Name(), StatusCode: resp.StatusCode, Message: fmt.Sprintf("list models failed: HTTP %d", resp.StatusCode)}
	}
	var v struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, &core.ProviderError{Code: core.ErrUnknown, Provider: a.Name(), Message: "decode models: " + err.Error()}
	}
	out := make([]core.ModelInfo, 0, len(v.Data))
	free := isLoopbackURL(a.cfg.BaseURL)
	for _, m := range v.Data {
		out = append(out, core.ModelInfo{
			ID: m.ID, Provider: a.Name(), DisplayName: m.ID,
			Caps:         core.Capabilities{SupportsTools: true, SupportsStreaming: true},
			Scores:       core.UnknownScores(),
			Availability: core.AvailAvailable,
			QuotaState:   "unknown",
			IsFree:       free,
			CostKnown:    free,
		})
	}
	return out, nil
}

type oaMessage struct {
	Role       string       `json:"role"`
	Content    string       `json:"content,omitempty"`
	Name       string       `json:"name,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
}

type oaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func toOpenAI(ms []core.Message) []oaMessage {
	out := make([]oaMessage, 0, len(ms))
	for _, m := range ms {
		o := oaMessage{Role: string(m.Role), Content: m.Content, Name: m.Name, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			var f oaToolCall
			f.ID = tc.ID
			f.Type = "function"
			f.Function.Name = tc.Name
			f.Function.Arguments = tc.Arguments
			o.ToolCalls = append(o.ToolCalls, f)
		}
		out = append(out, o)
	}
	return out
}

func (a *Adapter) chatBody(req core.ChatRequest, stream bool) map[string]any {
	body := map[string]any{
		"model": req.Model, "messages": toOpenAI(req.Messages), "stream": stream,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != 0 {
		body["temperature"] = req.Temperature
	}
	if len(req.Tools) > 0 {
		tools := make([]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			var schema any = map[string]any{}
			if t.Parameters != "" {
				_ = json.Unmarshal([]byte(t.Parameters), &schema)
			}
			tools = append(tools, map[string]any{
				"type":     "function",
				"function": map[string]any{"name": t.Name, "description": t.Description, "parameters": schema},
			})
		}
		body["tools"] = tools
	}
	return body
}

func (a *Adapter) doPost(ctx context.Context, body map[string]any) (*http.Response, []byte, error) {
	key, err := a.key()
	if err != nil && !a.keyless() {
		return nil, nil, err
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(raw))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, &core.ProviderError{Code: core.ErrNetwork, Provider: a.Name(), Message: err.Error()}
	}
	return resp, raw, nil
}

// Chat implements providers.Adapter.
func (a *Adapter) Chat(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
	resp, _, err := a.doPost(ctx, a.chatBody(req, false))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != 200 {
		code := core.ClassifyHTTPStatus(resp.StatusCode, string(body))
		return nil, &core.ProviderError{Code: code, Provider: a.Name(), Model: req.Model, StatusCode: resp.StatusCode, Message: fmt.Sprintf("chat failed: HTTP %d", resp.StatusCode)}
	}
	var v struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content   string       `json:"content"`
				ToolCalls []oaToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, &core.ProviderError{Code: core.ErrUnknown, Provider: a.Name(), Model: req.Model, Message: "decode chat response: " + err.Error()}
	}
	out := &core.ChatResponse{Model: req.Model, Provider: a.Name(), Usage: core.Usage{Input: v.Usage.PromptTokens, Output: v.Usage.CompletionTokens}}
	if len(v.Choices) > 0 {
		out.Content = v.Choices[0].Message.Content
		for _, tc := range v.Choices[0].Message.ToolCalls {
			out.ToolCalls = append(out.ToolCalls, core.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
		}
	}
	return out, nil
}

// Stream implements providers.Adapter (SSE).
func (a *Adapter) Stream(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error) {
	resp, _, err := a.doPost(ctx, a.chatBody(req, true))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		resp.Body.Close()
		code := core.ClassifyHTTPStatus(resp.StatusCode, string(body))
		return nil, &core.ProviderError{Code: code, Provider: a.Name(), Model: req.Model, StatusCode: resp.StatusCode, Message: fmt.Sprintf("stream failed: HTTP %d", resp.StatusCode)}
	}
	ch := make(chan core.StreamEvent, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		var usage core.Usage
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				ch <- core.StreamEvent{Type: core.EventDone, Usage: usage}
				return
			}
			var v struct {
				Choices []struct {
					Delta struct {
						Content          string       `json:"content"`
						ReasoningContent string       `json:"reasoning_content"`
						ToolCalls        []oaToolCall `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens     int64 `json:"prompt_tokens"`
					CompletionTokens int64 `json:"completion_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &v); err != nil {
				continue
			}
			if v.Usage != nil {
				usage.Input += v.Usage.PromptTokens
				usage.Output += v.Usage.CompletionTokens
			}
			for _, c := range v.Choices {
				if c.Delta.ReasoningContent != "" {
					ch <- core.StreamEvent{Type: core.EventReasoning, Delta: c.Delta.ReasoningContent}
				}
				if c.Delta.Content != "" {
					ch <- core.StreamEvent{Type: core.EventToken, Delta: c.Delta.Content}
				}
				for _, tc := range c.Delta.ToolCalls {
					ch <- core.StreamEvent{Type: core.EventTool, ToolCalls: []core.ToolCall{{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments}}}
				}
				if c.FinishReason != "" {
					ch <- core.StreamEvent{Type: core.EventDone, Usage: usage}
					return
				}
			}
		}
		if err := sc.Err(); err != nil {
			select {
			case ch <- core.StreamEvent{Type: core.EventError, Err: &core.ProviderError{Code: core.ErrNetwork, Provider: a.Name(), Message: err.Error()}}:
			case <-ctx.Done():
			}
			return
		}
		ch <- core.StreamEvent{Type: core.EventDone, Usage: usage}
	}()
	return ch, nil
}

// Health implements providers.Adapter. A missing key is reported, not fatal
// to the process: the router simply treats the backend as unavailable.
func (a *Adapter) Health(ctx context.Context) (providers.Health, error) {
	start := time.Now()
	if _, err := a.key(); err != nil {
		return providers.Health{OK: false, Message: "api key env not set", Checked: time.Now()}, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.cfg.BaseURL, "/")+"/models", nil)
	resp, err := a.client.Do(req)
	if err != nil {
		// Some OpenAI-compatible servers lack /models; fall back to TCP-level OK.
		return providers.Health{OK: true, Message: "reachable (model list unsupported)", Latency: time.Since(start), Checked: time.Now()}, nil
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return providers.Health{OK: false, Message: "authentication rejected", Checked: time.Now()}, nil
	}
	return providers.Health{OK: true, Message: "ok", Latency: time.Since(start), Checked: time.Now()}, nil
}
