package core

import (
	"strings"
	"testing"
)

func TestSessionPreservesStateAcrossSwitch(t *testing.T) {
	s := NewSession("s1", "fix the token refresh bug", "be helpful")
	s.AppendUser("inspect auth")
	s.AppendAssistant("reading session", []ToolCall{{ID: "c1", Name: "read", Arguments: `{"path":"src/auth/x.go"}`}})
	s.AppendTool("c1", "read", "file contents here", false)
	s.Plan = []PlanStep{{Title: "repro", Done: true}, {Title: "fix", Detail: "edit session.go"}}
	s.NoteFile("src/auth/x.go")
	s.NoteModelSwitch("opencode/a")
	s.NoteModelSwitch("kilo/b") // fallback happened; history must be intact

	if len(s.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(s.Messages))
	}
	tr := s.Transcript()
	for _, want := range []string{
		"fix the token refresh bug", "be helpful", "inspect auth",
		"reading session", "file contents here", "repro", "src/auth/x.go",
	} {
		if !strings.Contains(tr, want) {
			t.Errorf("transcript lost %q", want)
		}
	}
	if len(s.SwitchTrail) != 2 {
		t.Errorf("switch trail = %v", s.SwitchTrail)
	}
}

func TestAddUsage(t *testing.T) {
	s := NewSession("s", "t", "")
	s.AddUsage(Usage{Input: 100, Output: 20})
	s.AddUsage(Usage{Input: 50, Output: 5, Reasoning: 9})
	if s.TokensIn != 150 || s.TokensOut != 25 {
		t.Errorf("totals = %d/%d", s.TokensIn, s.TokensOut)
	}
}

func TestSnapshotDeepCopy(t *testing.T) {
	s := NewSession("s", "t", "")
	s.AppendUser("hi")
	cp := s.Snapshot()
	cp.Messages[0].Content = "mutated"
	if s.Messages[0].Content != "hi" {
		t.Error("snapshot shares message backing array")
	}
}
