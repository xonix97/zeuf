package cligw

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"zeuf/internal/core"
)

func TestParseReasoning(t *testing.T) {
	p := parseRunEvent(`{"type":"reasoning","part":{"type":"reasoning","text":"first thought"}}`)
	if p.reasoningDelta != "first thought" || p.textDelta != "" || p.perr != nil {
		t.Errorf("reasoning misparsed: %+v", p)
	}
	// Reasoning must never leak into answer text.
	var b strings.Builder
	if err := ParseRunLine(`{"type":"reasoning","part":{"type":"reasoning","text":"hmm"}}`, &b); err != nil || b.Len() != 0 {
		t.Errorf("reasoning leaked to text: %q %v", b.String(), err)
	}
}

func TestParseStepFinishUsage(t *testing.T) {
	p := parseRunEvent(`{"type":"step_finish","part":{"type":"step-finish","tokens":{"total":116,"input":98,"output":17,"reasoning":5}}}`)
	if p.usage == nil || p.usage.Input != 98 || p.usage.Output != 17 || p.usage.Reasoning != 5 {
		t.Errorf("usage misparsed: %+v", p.usage)
	}
	if p.textDelta != "" || p.perr != nil {
		t.Errorf("step_finish must be silent: %+v", p)
	}
}

func TestStreamReasoningUsageTools(t *testing.T) {
	dir := t.TempDir()
	fixture := dir + "/fakebin"
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"reasoning\",\"part\":{\"type\":\"reasoning\",\"text\":\"thinking it over\"}}'\n" +
		"printf '%s\\n' '{\"type\":\"text\",\"part\":{\"type\":\"text\",\"text\":\"final answer\"}}'\n" +
		"printf '%s\\n' '{\"type\":\"tool_use\",\"part\":{\"type\":\"tool\",\"tool\":\"bash\",\"state\":{\"status\":\"completed\",\"output\":\"ok\\n\",\"title\":\"run tests\",\"metadata\":{\"exit\":0}}}}'\n" +
		"printf '%s\\n' '{\"type\":\"step_finish\",\"part\":{\"type\":\"step-finish\",\"tokens\":{\"total\":60,\"input\":50,\"output\":10,\"reasoning\":2}}}'\n"
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	a := New(Backend{Binary: fixture, Provider: "fake", Workdir: dir, Timeout: 30 * time.Second})
	ch, err := a.Stream(context.Background(), core.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var sawReasoning, sawToken, sawTool bool
	var doneUsage core.Usage
	for ev := range ch {
		switch ev.Type {
		case core.EventReasoning:
			sawReasoning = true
			if !strings.Contains(ev.Delta, "thinking") {
				t.Errorf("reasoning delta = %q", ev.Delta)
			}
		case core.EventToken:
			sawToken = true
		case core.EventToolProgress:
			sawTool = true
			if ev.Tool != "bash" || !ev.Ok {
				t.Errorf("tool progress = %+v", ev)
			}
		case core.EventDone:
			doneUsage = ev.Usage
		case core.EventError:
			t.Fatalf("stream error: %v", ev.Err)
		}
	}
	if !sawReasoning || !sawToken || !sawTool {
		t.Errorf("missing events r=%v t=%v tool=%v", sawReasoning, sawToken, sawTool)
	}
	if doneUsage.Input != 50 || doneUsage.Output != 10 || doneUsage.Reasoning != 2 {
		t.Errorf("done usage = %+v", doneUsage)
	}
}

// TestPromptTravelsOnStdin: session content must reach the backend via
// stdin, never argv (ps-visible). The fixture records both channels.
func TestPromptTravelsOnStdin(t *testing.T) {
	dir := t.TempDir()
	seenStdin := dir + "/stdin.txt"
	seenArgs := dir + "/args.txt"
	fixture := dir + "/fakebin"
	script := "#!/bin/sh\n" +
		"cat > " + seenStdin + "\n" +
		"printf '%s' \"$*\" > " + seenArgs + "\n" +
		"printf '%s\\n' '{\"type\":\"text\",\"part\":{\"type\":\"text\",\"text\":\"ok\"}}'\n"
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	a := New(Backend{Binary: fixture, Provider: "fake", Workdir: dir, Timeout: 30 * time.Second})
	secret := "SECRET-MARKER-42"
	req := core.ChatRequest{Model: "m", Messages: []core.Message{{Role: core.RoleUser, Content: secret}}}
	if _, err := a.Chat(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	stdin, _ := os.ReadFile(seenStdin)
	if !strings.Contains(string(stdin), secret) {
		t.Errorf("prompt missing from stdin: %q", stdin)
	}
	argv, _ := os.ReadFile(seenArgs)
	if strings.Contains(string(argv), secret) {
		t.Errorf("session content leaked into argv: %q", argv)
	}
	if !strings.Contains(string(argv), "-m") {
		t.Errorf("model flag missing from argv: %q", argv)
	}
}

func TestParseToolUse(t *testing.T) {
	p := parseRunEvent(`{"type":"tool_use","part":{"type":"tool","tool":"bash","state":{"status":"completed","output":"DONE\n","title":"echo DONE","metadata":{"exit":0}}}}`)
	if p.tool == nil || p.tool.Name != "bash" || !p.tool.Ok {
		t.Errorf("tool_use misparsed: %+v", p.tool)
	}
	if p.tool != nil && !strings.Contains(p.tool.Preview, "DONE") {
		t.Errorf("preview wrong: %q", p.tool.Preview)
	}
	// Failed exit marks not-ok.
	q := parseRunEvent(`{"type":"tool_use","part":{"type":"tool","tool":"bash","state":{"status":"completed","output":"boom","metadata":{"exit":3}}}}`)
	if q.tool == nil || q.tool.Ok {
		t.Errorf("failed tool must be not-ok: %+v", q.tool)
	}
	// Non-completed states stay silent (no orphan spinners).
	r := parseRunEvent(`{"type":"tool_use","part":{"type":"tool","tool":"bash","state":{"status":"running"}}}`)
	if r.tool != nil {
		t.Errorf("running tool_use must stay silent: %+v", r.tool)
	}
}

const verboseSample = `opencode/big-pickle
{
  "id": "big-pickle",
  "providerID": "opencode",
  "name": "Big Pickle",
  "limit": {"context": 200000, "output": 32000},
  "capabilities": {"toolcall": true}
}
opencode/plain-free
{
  "id": "plain-free",
  "providerID": "opencode",
  "name": "Plain Free",
  "limit": {"context": 128000},
  "capabilities": {"toolcall": false}
}
`

func TestParseVerbose(t *testing.T) {
	ms := ParseVerbose("opencode", verboseSample)
	if len(ms) != 2 {
		t.Fatalf("parsed %d models, want 2", len(ms))
	}
	if ms[0].ID != "big-pickle" || ms[0].Caps.ContextLength != 200000 || !ms[0].Caps.SupportsTools {
		t.Errorf("first model wrong: %+v", ms[0])
	}
	if ms[1].Caps.SupportsTools {
		t.Errorf("second model should not support tools: %+v", ms[1])
	}
	for _, m := range ms {
		if m.Provider != "opencode" || m.QuotaState != "unknown" {
			t.Errorf("metadata wrong: %+v", m)
		}
		if m.Scores.Coding >= 0 {
			t.Error("scores must stay unknown (never faked)")
		}
	}
}

func TestParseFreeFacts(t *testing.T) {
	kiloOut := `kilo/cohere/north-mini-code:free
{
  "id": "cohere/north-mini-code:free",
  "providerID": "kilo",
  "name": "Cohere North",
  "limit": {"context": 128000},
  "capabilities": {"toolcall": true},
  "cost": {"input": 0, "output": 0},
  "isFree": true
}
kilo/anthropic/claude-opus-4.5
{
  "id": "anthropic/claude-opus-4.5",
  "providerID": "kilo",
  "name": "Claude Opus",
  "limit": {"context": 200000},
  "capabilities": {"toolcall": true},
  "cost": {"input": 5, "output": 25},
  "isFree": false
}
kilo/kilo-auto/frontier
{
  "id": "kilo-auto/frontier",
  "providerID": "kilo",
  "name": "Kilo Auto Frontier",
  "limit": {"context": 200000},
  "capabilities": {"toolcall": true},
  "cost": {"input": 0, "output": 0},
  "isFree": false
}
`
	ocOut := `opencode/mimo-v2.5-free
{
  "id": "mimo-v2.5-free",
  "providerID": "opencode",
  "name": "MiMo V2.5 Free",
  "limit": {"context": 200000},
  "capabilities": {"toolcall": true},
  "cost": {"input": 0, "output": 0}
}
`
	kms := ParseVerbose("kilo", kiloOut)
	oms := ParseVerbose("opencode", ocOut)
	if len(kms) != 3 || len(oms) != 1 {
		t.Fatalf("parsed kilo=%d opencode=%d", len(kms), len(oms))
	}
	if !kms[0].IsFree || !kms[0].CostKnown {
		t.Errorf("explicit isFree:true must be free+known: %+v", kms[0])
	}
	if kms[1].IsFree || !kms[1].CostKnown {
		t.Errorf("paid model must be known-paid, not free: %+v", kms[1])
	}
	if kms[2].IsFree {
		t.Errorf("explicit isFree:false with placeholder zero cost must NOT be free: %+v", kms[2])
	}
	if !oms[0].IsFree || !oms[0].CostKnown {
		t.Errorf("known zero cost must be free+known: %+v", oms[0])
	}
	// No pricing exposed at all: unknown, never guessed free from the name.
	plain := ParseVerbose("zz", "zz/super-free-thing\n")
	if len(plain) != 1 || plain[0].IsFree || plain[0].CostKnown {
		t.Errorf("name containing 'free' without pricing facts must stay unknown: %+v", plain)
	}
}
func TestParsePlainFallback(t *testing.T) {
	ms := ParseVerbose("kilo", "kilo/a\nkilo/b\n")
	if len(ms) != 2 || ms[0].ID != "a" || ms[1].Provider != "kilo" {
		t.Errorf("plain fallback wrong: %+v", ms)
	}
}

func TestParseRunLineText(t *testing.T) {
	var b strings.Builder
	line := `{"type":"text","part":{"type":"text","text":"hello "}}`
	if err := ParseRunLine(line, &b); err != nil {
		t.Fatal(err)
	}
	line2 := `{"type":"text","text":"world"}`
	if err := ParseRunLine(line2, &b); err != nil {
		t.Fatal(err)
	}
	if b.String() != "hello world" {
		t.Errorf("accumulated = %q", b.String())
	}
}

func TestParseRunLineError(t *testing.T) {
	var b strings.Builder
	err := ParseRunLine(`{"type":"error","error":{"name":"X","data":{"message":"rate limit exceeded, retry in 10s"}}}`, &b)
	if err == nil || err.Code != core.ErrRateLimited {
		t.Errorf("expected rate_limited, got %v", err)
	}
	err = ParseRunLine(`{"type":"error","error":{"name":"X","data":{"message":"insufficient_quota, out of credits"}}}`, &b)
	if err == nil || err.Code != core.ErrQuotaOut {
		t.Errorf("expected quota_exhausted, got %v", err)
	}
}

func TestPromptIncludesHistory(t *testing.T) {
	req := core.ChatRequest{Messages: []core.Message{
		{Role: core.RoleSystem, Content: "sys"},
		{Role: core.RoleUser, Content: "do the thing"},
		{Role: core.RoleAssistant, Content: "did step 1"},
		{Role: core.RoleTool, Name: "read", Content: "file data"},
	}}
	p := promptFor("/repo", req)
	for _, want := range []string{"sys", "do the thing", "did step 1", "file data"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lost %q", want)
		}
	}
	for _, want := range []string{
		"ACT on the workspace",
		"do not merely print code",
		"working directory: /repo",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing action framing %q", want)
		}
	}
	if strings.Contains(p, "no live tools") {
		t.Error("prompt must not claim the backend has no tools")
	}
}
