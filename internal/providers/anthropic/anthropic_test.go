package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"zeuf/internal/core"
)

func testServer(t *testing.T, handler http.HandlerFunc) (*Adapter, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Setenv("ZEUF_TEST_ANTHROPIC_KEY", "sk-ant-test-key")
	a := New(Config{Name: "anthropic", BaseURL: srv.URL, APIKeyEnv: "ZEUF_TEST_ANTHROPIC_KEY"})
	return a, srv.Close
}

func TestChatAndTools(t *testing.T) {
	a, done := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-ant-test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			http.Error(w, "bad version", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/messages" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{
				"model": "claude-3-7-sonnet-20250219",
				"content": [
					{"type": "text", "text": "I will read the file."},
					{"type": "tool_use", "id": "toolu_01", "name": "read", "input": {"path": "main.go"}}
				],
				"usage": {"input_tokens": 15, "output_tokens": 10}
			}`)
			return
		}
		http.NotFound(w, r)
	})
	defer done()

	resp, err := a.Chat(context.Background(), core.ChatRequest{
		Model: "claude-3-7-sonnet-20250219",
		Messages: []core.Message{
			{Role: core.RoleSystem, Content: "You are a coding assistant."},
			{Role: core.RoleUser, Content: "Read main.go"},
		},
		Tools: []core.ToolDef{
			{Name: "read", Description: "read file", Parameters: `{"type":"object","properties":{"path":{"type":"string"}}}`},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "I will read the file." {
		t.Errorf("content = %q, want 'I will read the file.'", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "toolu_01" || tc.Name != "read" || (tc.Arguments != `{"path":"main.go"}` && tc.Arguments != `{"path": "main.go"}`) {
		t.Errorf("tool call mismatch: %+v", tc)
	}
	if resp.Usage.Input != 15 || resp.Usage.Output != 10 {
		t.Errorf("usage mismatch: %+v", resp.Usage)
	}
}

func TestStreamTokensAndThinking(t *testing.T) {
	a, done := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\n")
		fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":50,\"output_tokens\":2}}}\n\n")
		fmt.Fprint(w, "event: content_block_start\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"Analyzing code structure...\"}}\n\n")
		fmt.Fprint(w, "event: content_block_stop\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		fmt.Fprint(w, "event: content_block_start\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello \"}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"world!\"}}\n\n")
		fmt.Fprint(w, "event: content_block_stop\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_stop\",\"index\":1}\n\n")
		fmt.Fprint(w, "event: message_delta\n")
		fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":20}}\n\n")
		fmt.Fprint(w, "event: message_stop\n")
		fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	})
	defer done()

	ch, err := a.Stream(context.Background(), core.ChatRequest{Model: "claude-3-7-sonnet-20250219"})
	if err != nil {
		t.Fatal(err)
	}
	var thinking, text string
	var usage core.Usage
	for ev := range ch {
		switch ev.Type {
		case core.EventReasoning:
			thinking += ev.Delta
		case core.EventToken:
			text += ev.Delta
		case core.EventDone:
			usage = ev.Usage
		case core.EventError:
			t.Fatal(ev.Err)
		}
	}
	if thinking != "Analyzing code structure..." {
		t.Errorf("thinking = %q", thinking)
	}
	if text != "Hello world!" {
		t.Errorf("text = %q, want 'Hello world!'", text)
	}
	if usage.Input != 50 || usage.Output != 20 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestStreamToolCall(t *testing.T) {
	a, done := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\n")
		fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_2\",\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n")
		fmt.Fprint(w, "event: content_block_start\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_test\",\"name\":\"bash\"}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"cmd\\\": \\\"\"}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"ls -la\\\"}\"}}\n\n")
		fmt.Fprint(w, "event: content_block_stop\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		fmt.Fprint(w, "event: message_stop\n")
		fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	})
	defer done()

	ch, err := a.Stream(context.Background(), core.ChatRequest{Model: "claude-3-7-sonnet-20250219"})
	if err != nil {
		t.Fatal(err)
	}
	var tools []core.ToolCall
	for ev := range ch {
		if ev.Type == core.EventTool {
			tools = append(tools, ev.ToolCalls...)
		}
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tools))
	}
	if tools[0].ID != "toolu_test" || tools[0].Name != "bash" || tools[0].Arguments != `{"cmd": "ls -la"}` {
		t.Errorf("unexpected tool: %+v", tools[0])
	}
}

func TestToAnthropicMessageConversion(t *testing.T) {
	msgs := []core.Message{
		{Role: core.RoleSystem, Content: "System doctrinal instruction"},
		{Role: core.RoleUser, Content: "First user request"},
		{Role: core.RoleAssistant, Content: "Running tool", ToolCalls: []core.ToolCall{{ID: "c1", Name: "exec", Arguments: `{"command":"pwd"}`}}},
		{Role: core.RoleTool, Content: "/home/user", ToolCallID: "c1", Name: "exec"},
		{Role: core.RoleUser, Content: "Next step please"},
	}
	sys, converted := toAnthropic(msgs)
	if sys != "System doctrinal instruction" {
		t.Errorf("system prompt mismatch: %q", sys)
	}
	if len(converted) == 0 {
		t.Fatal("empty converted messages")
	}
	if converted[0].Role != "user" {
		t.Errorf("first message must be user, got %s", converted[0].Role)
	}
}

func TestHealth(t *testing.T) {
	a, done := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"claude-3-7-sonnet-20250219","display_name":"Claude 3.7 Sonnet"}]}`)
			return
		}
		http.NotFound(w, r)
	})
	defer done()

	h, err := a.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !h.OK {
		t.Errorf("expected health OK, got %+v", h)
	}
}

func TestMissingKey(t *testing.T) {
	a := New(Config{Name: "anthropic", APIKeyEnv: "NON_EXISTENT_ENV_KEY_12345"})
	_, err := a.Chat(context.Background(), core.ChatRequest{Model: "claude-3-7-sonnet-20250219"})
	pe, ok := err.(*core.ProviderError)
	if !ok || pe.Code != core.ErrAuth {
		t.Errorf("expected ErrAuth, got %v", err)
	}
}
