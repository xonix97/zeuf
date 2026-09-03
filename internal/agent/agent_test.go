package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
	"zeuf/internal/providers/mock"
	"zeuf/internal/router"
)

func harness(t *testing.T, backends ...*mock.Adapter) (*Agent, *router.Router, *ct.Registry) {
	t.Helper()
	reg := router.NewRegistry()
	var es []router.Entry
	for _, b := range backends {
		reg.Register(b)
		ms, _ := b.ListModels(context.Background())
		for _, m := range ms {
			es = append(es, router.Entry{Model: m, Backend: b})
		}
	}
	reg.SetModels(es)
	r := router.New(reg)
	r.Backoff = 0
	tools := ct.NewRegistry(t.TempDir(), ct.Policy{AutoApprove: true})
	return New(r, tools), r, tools
}

// TestNativeToolLoop: the model asks to read a file, Zeuf executes it with
// structured results, the model answers. Milestone: real tool calls.
func TestNativeToolLoop(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("package main // v42"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := mock.New("m", []core.ModelInfo{mock.Model("m", "coder", 0.9, 200000, true)}, []mock.Script{
		{Resp: &core.ChatResponse{Content: "let me look", ToolCalls: []core.ToolCall{{ID: "c1", Name: "read", Arguments: `{"path":"app.go"}`}}}},
		{Resp: &core.ChatResponse{Content: "found v42"}},
	})
	ag, _, tools := harness(t, m)
	tools.Workdir = dir
	// Rebuild tool registry rooted at dir (Workdir is read at call time via resolve;
	// but NewRegistry captured dir — override by constructing fresh):
	tools2 := ct.NewRegistry(dir, ct.Policy{AutoApprove: true})
	ag.Tools = tools2

	sess := NewSession("s1", "check version", tools2)
	sess.AppendUser("check version")
	out, err := ag.RunTurn(ctx, sess, router.DefaultPrefs())
	if err != nil {
		t.Fatal(err)
	}
	if out != "found v42" {
		t.Errorf("final = %q", out)
	}
	// Structured tool result preserved in session.
	found := false
	for _, msg := range sess.Messages {
		if msg.Role == core.RoleTool && strings.Contains(msg.Content, "v42") {
			found = true
		}
	}
	if !found {
		t.Error("tool result missing from session history")
	}
	if len(sess.FilesInspected) == 0 {
		t.Error("inspected file not tracked")
	}
}

// TestAgentFallbackPreservesSession: quota death mid-task continues on the
// next model with history intact.
func TestAgentFallbackPreservesSession(t *testing.T) {
	ctx := context.Background()
	ma := mock.New("pa", []core.ModelInfo{mock.Model("pa", "a1", 0.95, 200000, true)}, []mock.Script{
		{Err: &core.ProviderError{Code: core.ErrQuotaOut, Provider: "pa", Message: "quota exhausted"}},
	})
	mb := mock.New("pb", []core.ModelInfo{mock.Model("pb", "b1", 0.5, 200000, true)}, []mock.Script{
		{Resp: &core.ChatResponse{Content: "continuing: fixed"}},
	})
	ag, r, tools := harness(t, ma, mb)
	var switched *router.SwitchInfo
	r.OnSwitch = func(s router.SwitchInfo) { switched = &s }

	sess := NewSession("s2", "fix bug", tools)
	sess.AppendUser("fix bug")
	out, err := ag.RunTurn(ctx, sess, router.DefaultPrefs())
	if err != nil {
		t.Fatal(err)
	}
	if out != "continuing: fixed" {
		t.Errorf("final = %q", out)
	}
	if switched == nil || !strings.Contains(switched.To, "b1") {
		t.Errorf("no switch notice: %+v", switched)
	}
	if len(sess.SwitchTrail) != 1 || !strings.Contains(sess.SwitchTrail[0], "b1") {
		t.Errorf("trail = %v", sess.SwitchTrail)
	}
	// The surviving model's request still carried the original user turn.
	if len(mb.Requests) == 0 {
		t.Fatal("backend-b never called")
	}
	joined := ""
	for _, m := range mb.Requests[0].Messages {
		joined += m.Content + "\n"
	}
	if !strings.Contains(joined, "fix bug") {
		t.Error("user task lost across fallback")
	}
}

// TestDelegateOrchestration: the orchestrator delegates a subtask to a
// subagent and folds the summary back into its own session.
func TestDelegateOrchestration(t *testing.T) {
	ctx := context.Background()
	m := mock.New("m", []core.ModelInfo{mock.Model("m", "orc", 0.9, 200000, true)}, []mock.Script{
		{Resp: &core.ChatResponse{Content: "delegating", ToolCalls: []core.ToolCall{{ID: "d1", Name: "delegate", Arguments: `{"task":"explore auth code","context":"repo root"}`}}}},
		{Resp: &core.ChatResponse{Content: "sub summary: token refresh missing"}}, // subagent turn
		{Resp: &core.ChatResponse{Content: "orchestrated fix ready"}},             // parent turn 2
	})
	ag, _, tools := harness(t, m)
	sess := NewSession("s4", "fix auth", tools)
	sess.AppendUser("fix auth")
	out, err := ag.RunTurn(ctx, sess, router.DefaultPrefs())
	if err != nil {
		t.Fatal(err)
	}
	if out != "orchestrated fix ready" {
		t.Errorf("final = %q", out)
	}
	joined := ""
	for _, msg := range sess.Messages {
		joined += msg.Content + "\n"
	}
	if !strings.Contains(joined, "sub summary: token refresh missing") {
		t.Error("subagent summary missing from orchestrator session")
	}
	// The subagent turn must not itself offer the delegate tool (depth cap).
	if len(m.Requests) < 2 {
		t.Fatalf("expected >=2 turns, got %d", len(m.Requests))
	}
	for _, td := range m.Requests[1].Tools {
		if td.Name == "delegate" {
			t.Error("subagent was offered the delegate tool (nesting must stop at depth 1)")
		}
	}
	if len(m.Requests[0].Tools) == 0 {
		t.Error("orchestrator turn should include tool definitions")
	}
}

// TestAgentStreamsTokens: streaming delivers content even without Chat.
func TestAgentStreamsTokens(t *testing.T) {
	ctx := context.Background()
	m := mock.New("m", []core.ModelInfo{mock.Model("m", "s", 0.5, 0, false)}, []mock.Script{
		{Resp: &core.ChatResponse{Content: "streamed hello"}},
	})
	ag, _, tools := harness(t, m)
	var tokens strings.Builder
	ag.Emit = func(ev Event) {
		if ev.Type == EvToken {
			tokens.WriteString(ev.Text)
		}
	}
	sess := NewSession("s3", "hi", tools)
	sess.AppendUser("hi")
	if _, err := ag.RunTurn(ctx, sess, router.DefaultPrefs()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tokens.String(), "streamed") {
		t.Errorf("no tokens streamed: %q", tokens.String())
	}
}

// TestParallelToolOrder: concurrent tool results commit in call order.
func TestParallelToolOrder(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("AAA"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("BBB"), 0o644)
	m := mock.New("m", []core.ModelInfo{mock.Model("m", "p", 0.9, 200000, true)}, []mock.Script{
		{Resp: &core.ChatResponse{Content: "reading both", ToolCalls: []core.ToolCall{
			{ID: "c1", Name: "read", Arguments: `{"path":"a.txt"}`},
			{ID: "c2", Name: "read", Arguments: `{"path":"b.txt"}`},
		}}},
		{Resp: &core.ChatResponse{Content: "done"}},
	})
	ag, _, _ := harness(t, m)
	tools := ct.NewRegistry(dir, ct.Policy{AutoApprove: true})
	ag.Tools = tools
	sess := NewSession("s5", "read both", tools)
	sess.AppendUser("read both")
	if _, err := ag.RunTurn(ctx, sess, router.DefaultPrefs()); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, msg := range sess.Messages {
		if msg.Role == core.RoleTool {
			got = append(got, msg.ToolCallID+":"+strings.TrimSpace(msg.Content))
		}
	}
	if len(got) != 2 || !strings.HasPrefix(got[0], "c1:") || !strings.Contains(got[0], "AAA") ||
		!strings.HasPrefix(got[1], "c2:") || !strings.Contains(got[1], "BBB") {
		t.Errorf("tool results out of order or wrong: %q", got)
	}
}

// TestApprovalHub blocks the loop until the UI answers.
func TestApprovalHub(t *testing.T) {
	hub := NewHub()
	done := make(chan bool, 1)
	go func() { done <- hub.Ask("write file", "/tmp/x") }()
	var req *ApprovalReq
	select {
	case req = <-hub.Requests:
	case <-time.After(2 * time.Second):
		t.Fatal("no approval request arrived")
	}
	if req.Action != "write file" {
		t.Errorf("action = %q", req.Action)
	}
	req.Resp <- true
	select {
	case ok := <-done:
		if !ok {
			t.Error("expected approval")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask did not return")
	}
}
