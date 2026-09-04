// Package anthropic implements the native Anthropic HTTP adapter using
// the official /v1/messages protocol. Zeuf owns the full agent loop:
// chat, tool calling with schema normalization, and SSE streaming
// (including Claude 3.7 extended thinking tokens) communicate directly
// with the Anthropic API.
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"zeuf/internal/auth"
	"zeuf/internal/core"
	"zeuf/internal/providers"
)

const (
	defaultBaseURL   = "https://api.anthropic.com/v1"
	defaultKeyEnv    = "ANTHROPIC_API_KEY"
	anthropicVersion = "2023-06-01"
)

// Config describes one Anthropic endpoint.
type Config struct {
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	APIKeyEnv  string `json:"api_key_env"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

// Adapter is a native Anthropic backend.
type Adapter struct {
	cfg    Config
	client *http.Client
}

// New builds the Anthropic adapter.
func New(cfg Config) *Adapter {
	if cfg.Name == "" {
		cfg.Name = "anthropic"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.APIKeyEnv == "" {
		cfg.APIKeyEnv = defaultKeyEnv
	}
	timeout := 180 * time.Second
	if cfg.TimeoutSec > 0 {
		timeout = time.Duration(cfg.TimeoutSec) * time.Second
	}
	return &Adapter{cfg: cfg, client: &http.Client{Timeout: timeout}}
}

// Name implements providers.Adapter.
func (a *Adapter) Name() string {
	if a.cfg.Name == "" || a.cfg.Name == "anthropic" {
		return "anthropic"
	}
	return "anthropic:" + a.cfg.Name
}

// Delegated implements providers.Adapter (Zeuf owns the full agent loop).
func (a *Adapter) Delegated() bool { return false }

func (a *Adapter) key() (string, error) {
	if a.cfg.APIKeyEnv != "" {
		if k := strings.TrimSpace(os.Getenv(a.cfg.APIKeyEnv)); k != "" {
			return k, nil
		}
	}
	// Fall back to Zeuf auth store.
	store, _ := auth.Open()
	if store != nil {
		if k, err := store.Get(auth.ServiceDirect, a.cfg.Name); err == nil && strings.TrimSpace(k) != "" {
			return strings.TrimSpace(k), nil
		}
		if a.cfg.Name != "anthropic" {
			if k, err := store.Get(auth.ServiceDirect, "anthropic"); err == nil && strings.TrimSpace(k) != "" {
				return strings.TrimSpace(k), nil
			}
		}
	}
	return "", &core.ProviderError{
		Code:     core.ErrAuth,
		Provider: a.Name(),
		Message:  fmt.Sprintf("no Anthropic API key: %s is not set and no stored credential exists", a.cfg.APIKeyEnv),
	}
}

// KnownModels returns known Anthropic models with their capabilities.
func KnownModels() []core.ModelInfo {
	mk := func(id, display string, coding, reasoning, quality, latency float64) core.ModelInfo {
		return core.ModelInfo{
			ID:          id,
			Provider:    "anthropic",
			DisplayName: display,
			Caps: core.Capabilities{
				ContextLength:     200000,
				SupportsTools:     true,
				SupportsStreaming: true,
			},
			Scores: core.Scores{
				Coding:    coding,
				Reasoning: reasoning,
				Quality:   quality,
				Latency:   latency,
			},
			Availability: core.AvailAvailable,
			QuotaState:   "unknown",
			IsFree:       false,
			CostKnown:    true,
		}
	}
	return []core.ModelInfo{
		mk("claude-3-7-sonnet-20250219", "Claude 3.7 Sonnet", 0.96, 0.95, 0.95, 0.80),
		mk("claude-3-5-sonnet-20241022", "Claude 3.5 Sonnet", 0.93, 0.91, 0.92, 0.85),
		mk("claude-3-5-haiku-20241022", "Claude 3.5 Haiku", 0.84, 0.80, 0.82, 0.95),
		mk("claude-3-opus-20240229", "Claude 3 Opus", 0.88, 0.88, 0.90, 0.60),
	}
}

// ListModels implements providers.Adapter via GET /v1/models with fallback to KnownModels.
func (a *Adapter) ListModels(ctx context.Context) ([]core.ModelInfo, error) {
	key, err := a.key()
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.cfg.BaseURL, "/")+"/models", nil)
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := a.client.Do(req)
	if err != nil {
		return KnownModels(), nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return KnownModels(), nil
	}

	var v struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &v); err != nil || len(v.Data) == 0 {
		return KnownModels(), nil
	}

	knownMap := map[string]core.ModelInfo{}
	for _, km := range KnownModels() {
		knownMap[km.ID] = km
	}

	out := make([]core.ModelInfo, 0, len(v.Data))
	for _, m := range v.Data {
		disp := m.DisplayName
		if disp == "" {
			disp = m.ID
		}
		mi, ok := knownMap[m.ID]
		if !ok {
			mi = core.ModelInfo{
				ID:          m.ID,
				Provider:    a.Name(),
				DisplayName: disp,
				Caps: core.Capabilities{
					ContextLength:     200000,
					SupportsTools:     true,
					SupportsStreaming: true,
				},
				Scores:       core.UnknownScores(),
				Availability: core.AvailAvailable,
				QuotaState:   "unknown",
				CostKnown:    true,
			}
		} else {
			mi.Provider = a.Name()
			mi.DisplayName = disp
		}
		out = append(out, mi)
	}
	return out, nil
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []any
}

func toAnthropic(ms []core.Message) (string, []anthropicMessage) {
	var systemParts []string
	var out []anthropicMessage

	type contentBlock map[string]any

	for _, m := range ms {
		switch m.Role {
		case core.RoleSystem:
			if strings.TrimSpace(m.Content) != "" {
				systemParts = append(systemParts, m.Content)
			}

		case core.RoleUser:
			var blocks []contentBlock
			if m.Content != "" {
				blocks = append(blocks, contentBlock{"type": "text", "text": m.Content})
			}
			// If previous message was also "user", merge blocks into it
			if len(out) > 0 && out[len(out)-1].Role == "user" {
				prev := out[len(out)-1]
				if prevList, ok := prev.Content.([]contentBlock); ok {
					out[len(out)-1].Content = append(prevList, blocks...)
					continue
				}
			}
			out = append(out, anthropicMessage{Role: "user", Content: blocks})

		case core.RoleAssistant:
			var blocks []contentBlock
			if m.Content != "" {
				blocks = append(blocks, contentBlock{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var input any = map[string]any{}
				if tc.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Arguments), &input)
				}
				blocks = append(blocks, contentBlock{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": input,
				})
			}
			if len(blocks) == 0 {
				blocks = append(blocks, contentBlock{"type": "text", "text": ""})
			}
			// If previous message was assistant, merge blocks
			if len(out) > 0 && out[len(out)-1].Role == "assistant" {
				prev := out[len(out)-1]
				if prevList, ok := prev.Content.([]contentBlock); ok {
					out[len(out)-1].Content = append(prevList, blocks...)
					continue
				}
			}
			out = append(out, anthropicMessage{Role: "assistant", Content: blocks})

		case core.RoleTool:
			// Tool results in Anthropic must be sent as a user message with tool_result blocks.
			tb := contentBlock{
				"type":        "tool_result",
				"tool_use_id": m.ToolCallID,
				"content":     m.Content,
			}
			if len(out) > 0 && out[len(out)-1].Role == "user" {
				if prevList, ok := out[len(out)-1].Content.([]contentBlock); ok {
					out[len(out)-1].Content = append(prevList, tb)
					continue
				}
			}
			out = append(out, anthropicMessage{Role: "user", Content: []contentBlock{tb}})
		}
	}

	// Anthropic requires the first message to be "user".
	if len(out) == 0 || out[0].Role != "user" {
		out = append([]anthropicMessage{{Role: "user", Content: []contentBlock{{"type": "text", "text": "Begin."}}}}, out...)
	}

	return strings.Join(systemParts, "\n\n"), out
}

func (a *Adapter) chatBody(req core.ChatRequest, stream bool) map[string]any {
	systemPrompt, messages := toAnthropic(req.Messages)
	model := req.Model
	if model == "" {
		model = "claude-3-7-sonnet-20250219"
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	body := map[string]any{
		"model":      model,
		"messages":   messages,
		"max_tokens": maxTokens,
		"stream":     stream,
	}
	if systemPrompt != "" {
		body["system"] = systemPrompt
	}
	if req.Temperature != 0 {
		body["temperature"] = req.Temperature
	}
	if len(req.Tools) > 0 {
		tools := make([]anthropicTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			var schema map[string]any
			if t.Parameters != "" {
				_ = json.Unmarshal([]byte(t.Parameters), &schema)
			}
			if schema == nil {
				schema = map[string]any{"type": "object"}
			}
			tools = append(tools, anthropicTool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: schema,
			})
		}
		body["tools"] = tools
	}
	return body
}

func (a *Adapter) doPost(ctx context.Context, body map[string]any) (*http.Response, []byte, error) {
	key, err := a.key()
	if err != nil {
		return nil, nil, err
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.cfg.BaseURL, "/")+"/messages", bytes.NewReader(raw))
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", anthropicVersion)
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
	if resp.StatusCode != http.StatusOK {
		code := core.ClassifyHTTPStatus(resp.StatusCode, string(body))
		return nil, &core.ProviderError{
			Code:       code,
			Provider:   a.Name(),
			Model:      req.Model,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("anthropic chat failed: HTTP %d: %s", resp.StatusCode, string(body)),
		}
	}

	var v struct {
		Model   string `json:"model"`
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text,omitempty"`
			ID    string          `json:"id,omitempty"`
			Name  string          `json:"name,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
		} `json:"content"`
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, &core.ProviderError{
			Code:     core.ErrUnknown,
			Provider: a.Name(),
			Model:    req.Model,
			Message:  "decode anthropic chat response: " + err.Error(),
		}
	}

	out := &core.ChatResponse{
		Model:    v.Model,
		Provider: a.Name(),
		Usage:    core.Usage{Input: v.Usage.InputTokens, Output: v.Usage.OutputTokens},
	}
	for _, c := range v.Content {
		switch c.Type {
		case "text":
			out.Content += c.Text
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, core.ToolCall{
				ID:        c.ID,
				Name:      c.Name,
				Arguments: string(c.Input),
			})
		}
	}
	return out, nil
}

// Stream implements providers.Adapter (Anthropic SSE).
func (a *Adapter) Stream(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error) {
	resp, _, err := a.doPost(ctx, a.chatBody(req, true))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		resp.Body.Close()
		code := core.ClassifyHTTPStatus(resp.StatusCode, string(body))
		return nil, &core.ProviderError{
			Code:       code,
			Provider:   a.Name(),
			Model:      req.Model,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("anthropic stream failed: HTTP %d: %s", resp.StatusCode, string(body)),
		}
	}

	ch := make(chan core.StreamEvent, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		var usage core.Usage
		type toolBuilder struct {
			id   string
			name string
			args strings.Builder
		}
		tools := map[int]*toolBuilder{}
		var toolOrder []int

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
				break
			}

			var v struct {
				Type    string `json:"type"`
				Message *struct {
					Usage struct {
						InputTokens  int64 `json:"input_tokens"`
						OutputTokens int64 `json:"output_tokens"`
					} `json:"usage"`
				} `json:"message"`
				Index        int `json:"index"`
				ContentBlock *struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
				Delta *struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					Thinking    string `json:"thinking"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
				Usage *struct {
					OutputTokens int64 `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &v); err != nil {
				continue
			}

			switch v.Type {
			case "message_start":
				if v.Message != nil {
					usage.Input += v.Message.Usage.InputTokens
					usage.Output += v.Message.Usage.OutputTokens
				}

			case "content_block_start":
				if v.ContentBlock != nil && v.ContentBlock.Type == "tool_use" {
					tb := &toolBuilder{id: v.ContentBlock.ID, name: v.ContentBlock.Name}
					tools[v.Index] = tb
					toolOrder = append(toolOrder, v.Index)
				}

			case "content_block_delta":
				if v.Delta != nil {
					if v.Delta.Type == "text_delta" && v.Delta.Text != "" {
						ch <- core.StreamEvent{Type: core.EventToken, Delta: v.Delta.Text}
					} else if v.Delta.Type == "thinking_delta" && v.Delta.Thinking != "" {
						ch <- core.StreamEvent{Type: core.EventReasoning, Delta: v.Delta.Thinking}
					} else if v.Delta.Type == "input_json_delta" && v.Delta.PartialJSON != "" {
						if tb := tools[v.Index]; tb != nil {
							tb.args.WriteString(v.Delta.PartialJSON)
						}
					}
				}

			case "message_delta":
				if v.Usage != nil {
					usage.Output = v.Usage.OutputTokens
				}

			case "message_stop":
				if len(toolOrder) > 0 {
					var calls []core.ToolCall
					for _, idx := range toolOrder {
						if tb := tools[idx]; tb != nil {
							calls = append(calls, core.ToolCall{
								ID:        tb.id,
								Name:      tb.name,
								Arguments: tb.args.String(),
							})
						}
					}
					ch <- core.StreamEvent{Type: core.EventTool, ToolCalls: calls}
					toolOrder = nil
				}
				ch <- core.StreamEvent{Type: core.EventDone, Usage: usage}
				return
			}
		}

		if err := sc.Err(); err != nil {
			select {
			case ch <- core.StreamEvent{
				Type: core.EventError,
				Err:  &core.ProviderError{Code: core.ErrNetwork, Provider: a.Name(), Message: err.Error()},
			}:
			case <-ctx.Done():
			}
			return
		}

		if len(toolOrder) > 0 {
			var calls []core.ToolCall
			for _, idx := range toolOrder {
				if tb := tools[idx]; tb != nil {
					calls = append(calls, core.ToolCall{
						ID:        tb.id,
						Name:      tb.name,
						Arguments: tb.args.String(),
					})
				}
			}
			ch <- core.StreamEvent{Type: core.EventTool, ToolCalls: calls}
		}
		ch <- core.StreamEvent{Type: core.EventDone, Usage: usage}
	}()

	return ch, nil
}

// Health implements providers.Adapter.
func (a *Adapter) Health(ctx context.Context) (providers.Health, error) {
	start := time.Now()
	key, err := a.key()
	if err != nil {
		return providers.Health{OK: false, Message: "api key env not set", Checked: time.Now()}, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.cfg.BaseURL, "/")+"/models", nil)
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := a.client.Do(req)
	if err != nil {
		return providers.Health{OK: false, Message: "connection failed: " + err.Error(), Latency: time.Since(start), Checked: time.Now()}, nil
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return providers.Health{OK: false, Message: "authentication rejected", Checked: time.Now()}, nil
	}
	return providers.Health{OK: true, Message: "ok", Latency: time.Since(start), Models: len(KnownModels()), Checked: time.Now()}, nil
}
