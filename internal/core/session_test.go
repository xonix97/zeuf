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

func TestSessionStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	s := NewSession("s-1", "fix things", "")
	s.AppendUser("hi")
	s.AppendAssistant("done", nil)
	s.AddUsage(Usage{Input: 10, Output: 2})
	s.AddCheckpoint(Checkpoint{Label: "t", Files: []FileVersion{{Path: "/x", Before: "old", Existed: true}}})
	s.Meta["workdir"] = dir
	if err := SaveSession(s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSession("s-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 || got.TokensIn != 10 || len(got.Checkpoints) != 1 {
		t.Errorf("roundtrip lost state: %+v", got)
	}
	if _, err := LoadSession("../evil"); err == nil {
		t.Error("traversal id must fail")
	}
}

func TestListSessions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if list, err := ListSessions(); err != nil || len(list) != 0 {
		t.Fatalf("empty dir = %v %v", list, err)
	}
	a := NewSession("a", "first", "")
	a.AppendUser("x")
	if err := SaveSession(a); err != nil {
		t.Fatal(err)
	}
	b := NewSession("b", "second", "")
	b.AppendUser("x")
	b.AppendUser("y")
	if err := SaveSession(b); err != nil {
		t.Fatal(err)
	}
	list, err := ListSessions()
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %v %v", list, err)
	}
	if list[0].ID != "b" || list[0].Turns != 2 || list[1].ID != "a" {
		t.Errorf("order/counts wrong: %+v", list)
	}
}
