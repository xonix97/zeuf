package direct

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
	t.Setenv("ZEUF_TEST_KEY", "test-key-value")
	a := New(Config{Name: "test", BaseURL: srv.URL, APIKeyEnv: "ZEUF_TEST_KEY"})
	return a, srv.Close
}

func TestChatAndTools(t *testing.T) {
	a, done := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" {
			fmt.Fprint(w, `{"model":"m","choices":[{"message":{"content":"hi","tool_calls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{}"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
			return
		}
		http.NotFound(w, r)
	})
	defer done()
	resp, err := a.Chat(context.Background(), core.ChatRequest{Model: "m", Messages: []core.Message{{Role: core.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hi" || len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "read" {
		t.Errorf("chat mapping wrong: %+v", resp)
	}
	if resp.Usage.Input != 10 || resp.Usage.Output != 5 {
		t.Errorf("usage wrong: %+v", resp.Usage)
	}
}

func TestStreamTokens(t *testing.T) {
	a, done := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer done()
	ch, err := a.Stream(context.Background(), core.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for ev := range ch {
		if ev.Type == core.EventToken {
			got += ev.Delta
		}
		if ev.Type == core.EventError {
			t.Fatal(ev.Err)
		}
	}
	if got != "hello" {
		t.Errorf("streamed = %q, want hello", got)
	}
}

func TestStreamReasoningAndUsage(t *testing.T) {
	a, done := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"let me think\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer done()
	ch, err := a.Stream(context.Background(), core.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var reasoning, text string
	var usage core.Usage
	for ev := range ch {
		switch ev.Type {
		case core.EventReasoning:
			reasoning += ev.Delta
		case core.EventToken:
			text += ev.Delta
		case core.EventDone:
			usage = ev.Usage
		case core.EventError:
			t.Fatal(ev.Err)
		}
	}
	if reasoning != "let me think" {
		t.Errorf("reasoning = %q", reasoning)
	}
	if text != "hi" {
		t.Errorf("text = %q", text)
	}
	if usage.Input != 100 || usage.Output != 20 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestIsLoopbackURL(t *testing.T) {
	free := []string{
		"http://localhost:11434/v1",
		"http://127.0.0.1:1234/v1",
		"http://[::1]:11434/v1",
		"https://localhost:443/x",
	}
	for _, u := range free {
		if !isLoopbackURL(u) {
			t.Errorf("%s should count as loopback", u)
		}
	}
	metered := []string{
		"https://openrouter.ai/api/v1",
		"https://api.openai.com/v1",
		"http://192.168.1.10:11434/v1",
		"http://10.0.0.5/v1",
		"http://0.0.0.0:1234/v1",
		"not a url at all",
		"",
	}
	for _, u := range metered {
		if isLoopbackURL(u) {
			t.Errorf("%s must NOT count as loopback", u)
		}
	}
}

func TestLoopbackModelsAreFree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"fable-5:latest"}]}`)
	}))
	defer srv.Close()
	a := New(Config{Name: "ollama", BaseURL: srv.URL + "/v1", APIKeyEnv: ""})
	ms, err := a.ListModels(context.Background())
	if err != nil {
		t.Fatalf("keyless local listing failed: %v", err)
	}
	if len(ms) != 1 || !ms[0].IsFree || !ms[0].CostKnown {
		t.Errorf("loopback models must be affirmatively free: %+v", ms)
	}
}

func TestKeylessChatOmitsAuthHeader(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"model":"m","choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer srv.Close()
	a := New(Config{Name: "ollama", BaseURL: srv.URL + "/v1", APIKeyEnv: ""})
	resp, err := a.Chat(context.Background(), core.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hi" {
		t.Errorf("content = %q", resp.Content)
	}
	if sawAuth != "" {
		t.Errorf("keyless request sent auth %q", sawAuth)
	}
}

func TestRateLimitAndQuotaMapping(t *testing.T) {
	a, done := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"message":"insufficient_quota, out of credits"}}`)
	})
	defer done()
	_, err := a.Chat(context.Background(), core.ChatRequest{Model: "m"})
	pe, ok := err.(*core.ProviderError)
	if !ok || pe.Code != core.ErrQuotaOut {
		t.Errorf("expected quota_exhausted, got %v", err)
	}
}

func TestMissingKeyIsAuthError(t *testing.T) {
	a := New(Config{Name: "x", BaseURL: "http://127.0.0.1:1", APIKeyEnv: "ZEUF_MISSING_KEY_xyz"})
	_, err := a.Chat(context.Background(), core.ChatRequest{Model: "m"})
	if pe, ok := err.(*core.ProviderError); !ok || pe.Code != core.ErrAuth {
		t.Errorf("expected auth error, got %v", err)
	}
}
