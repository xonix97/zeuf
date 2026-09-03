package cligw

import (
	"strings"
	"testing"

	"zeuf/internal/core"
)

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
	p := promptFor(req)
	for _, want := range []string{"sys", "do the thing", "did step 1", "file data"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lost %q", want)
		}
	}
}
