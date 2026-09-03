package gemini

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"zeuf/internal/core"
)

func TestKnownModelsHonest(t *testing.T) {
	ms := KnownModels()
	if len(ms) < 10 {
		t.Fatalf("expected documented IDs, got %d", len(ms))
	}
	byID := map[string]core.ModelInfo{}
	for _, m := range ms {
		if m.ID == "" || m.Provider != "gemini" {
			t.Errorf("bad model entry: %+v", m)
		}
		if _, dup := byID[m.ID]; dup {
			t.Errorf("duplicate model %s", m.ID)
		}
		byID[m.ID] = m
		if m.Scores.Coding >= 0 || m.QuotaState != "unknown" {
			t.Errorf("model must not fake scores/quota: %+v", m)
		}
	}
	for _, want := range []string{
		"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite",
		"gemini-2.0-flash", "gemini-3-pro-preview", "gemini-3-flash-preview",
		"gemini-3.1-pro", "gemini-3.5-flash", "gemini-3.5-flash-lite",
		"gemini-3.6-flash",
	} {
		if _, ok := byID[want]; !ok {
			t.Errorf("documented model %s missing", want)
		}
	}
	// Free-tier family only; pro/paid models stay honestly unmarked.
	for _, id := range []string{"gemini-2.5-flash", "gemini-2.5-flash-lite", "gemini-2.0-flash", "gemini-3.5-flash-lite", "gemini-3.6-flash"} {
		if !byID[id].IsFree {
			t.Errorf("%s should be free-tier marked", id)
		}
	}
	for _, id := range []string{"gemini-2.5-pro", "gemini-3-pro-preview", "gemini-3.1-pro", "gemini-3.5-flash"} {
		if byID[id].IsFree {
			t.Errorf("%s must not be marked free", id)
		}
	}
}

func TestListModelsUnauthenticated(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "")
	t.Setenv("GOOGLE_GENAI_USE_GCA", "")
	t.Setenv("HOME", t.TempDir()) // no oauth file can exist here
	a := New(Config{})
	ms, err := a.ListModels(context.Background())
	if err != nil {
		t.Fatalf("discovery must not fail without auth: %v", err)
	}
	if len(ms) == 0 {
		t.Fatal("no models listed")
	}
	for _, m := range ms {
		if m.Availability != core.AvailAuthError || m.LastError == "" {
			t.Errorf("unauthenticated model must say so: %+v", m)
		}
	}
	if h, _ := a.Health(context.Background()); h.OK {
		t.Errorf("health must report login needed: %+v", h)
	}
}

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gemini")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestChatJSON(t *testing.T) {
	fix := writeFixture(t, `printf '%s' '{"session_id":"s1","response":"hello there","stats":{"input_tokens":10,"output_tokens":4}}'`)
	a := New(Config{Binary: fix, Timeout: 30 * time.Second})
	resp, err := a.Chat(context.Background(), core.ChatRequest{Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hello there" {
		t.Errorf("content = %q", resp.Content)
	}
	if resp.Usage.Input != 10 || resp.Usage.Output != 4 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestChatError(t *testing.T) {
	fix := writeFixture(t, `printf '%s' '{"session_id":"s1","error":{"type":"Error","message":"Please set an Auth method"}}'; exit 1`)
	a := New(Config{Binary: fix, Timeout: 30 * time.Second})
	_, err := a.Chat(context.Background(), core.ChatRequest{Model: "gemini-2.5-flash"})
	pe, ok := err.(*core.ProviderError)
	if !ok || pe.Code != core.ErrAuth {
		t.Errorf("expected auth failure, got %v", err)
	}
}

func TestStreamJSONL(t *testing.T) {
	fix := writeFixture(t, `printf '%s\n' \
 '{"type":"init","session_id":"s1","model":"gemini-2.5-flash"}' \
 '{"type":"message","content":"hel"}' \
 '{"type":"message","content":"lo"}' \
 '{"type":"tool_use","callId":"c1","name":"read","title":"Read a.go"}' \
 '{"type":"tool_result","callId":"c1","output":"package main"}' \
 '{"type":"result","stats":{"input_tokens":100,"output_tokens":20}}'`)
	a := New(Config{Binary: fix, Timeout: 30 * time.Second})
	ch, err := a.Stream(context.Background(), core.ChatRequest{Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	var starts, ends int
	var usage core.Usage
	for ev := range ch {
		switch ev.Type {
		case core.EventToken:
			text += ev.Delta
		case core.EventToolProgress:
			if ev.Done {
				ends++
				if ev.Tool != "read" || !ev.Ok {
					t.Errorf("tool end = %+v", ev)
				}
			} else {
				starts++
			}
		case core.EventDone:
			usage = ev.Usage
		case core.EventError:
			t.Fatalf("stream error: %v", ev.Err)
		}
	}
	if text != "hello" {
		t.Errorf("text = %q", text)
	}
	if starts != 1 || ends != 1 {
		t.Errorf("tool start/end = %d/%d", starts, ends)
	}
	if usage.Input != 100 || usage.Output != 20 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestStreamErrorWithoutResult(t *testing.T) {
	fix := writeFixture(t, `printf '%s\n' '{"type":"error","message":"quota exhausted for today"}'; exit 1`)
	a := New(Config{Binary: fix, Timeout: 30 * time.Second})
	ch, err := a.Stream(context.Background(), core.ChatRequest{Model: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var got error
	for ev := range ch {
		if ev.Type == core.EventError {
			got = ev.Err
		}
	}
	pe, ok := got.(*core.ProviderError)
	if !ok || pe.Code != core.ErrQuotaOut {
		t.Errorf("expected quota failure, got %v", got)
	}
}

func TestMessageTextVariants(t *testing.T) {
	cases := map[string]string{
		`{"text":"a"}`:                       "a",
		`{"content":"b"}`:                    "b",
		`{"response":"c"}`:                   "c",
		`{"content":[{"text":"d"},{"x":1}]}`: "d",
		`{"message":{"content":"e"}}`:        "e",
		`{"delta":"f"}`:                      "f",
	}
	for in, want := range cases {
		var v map[string]any
		if err := json.Unmarshal([]byte(in), &v); err != nil {
			t.Fatal(err)
		}
		if got := messageText(v); got != want {
			t.Errorf("messageText(%s) = %q, want %q", in, got, want)
		}
	}
	if got := messageText(map[string]any{}); got != "" {
		t.Errorf("empty map = %q", got)
	}
}

func TestStatsVariants(t *testing.T) {
	var v map[string]any
	if err := json.Unmarshal([]byte(`{"prompt_tokens":5,"completion_tokens":7}`), &v); err != nil {
		t.Fatal(err)
	}
	if u := statsUsage(v); u.Input != 5 || u.Output != 7 {
		t.Errorf("usage = %+v", u)
	}
	var v2 map[string]any
	if err := json.Unmarshal([]byte(`{"tokens":{"inputTokens":3,"outputTokens":4}}`), &v2); err != nil {
		t.Fatal(err)
	}
	if u := statsUsage(v2); u.Input != 3 || u.Output != 4 {
		t.Errorf("nested usage = %+v", u)
	}
}
