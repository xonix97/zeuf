package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
	"zeuf/internal/providers/mock"
	"zeuf/internal/router"
)

// TestOrchestrationWorkflow tests the complete pipeline:
// Intake -> Discovery -> Planning -> Concurrent Scheduling -> Specialist Subagent -> Verification -> Completion.
func TestOrchestrationWorkflow(t *testing.T) {
	dir := t.TempDir()
	tools := ct.NewRegistry(dir, ct.Policy{AutoApprove: true})

	// Create initial file
	if err := os.WriteFile(filepath.Join(dir, "calc.go"), []byte("package calc\nfunc Add(a, b int) int { return a + b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	planJSON := `{
		"goal": "Add Multiply to calc.go and verify",
		"tasks": [
			{
				"id": "T1",
				"title": "Inspect calc.go",
				"description": "Read calc.go to check existing function structure",
				"assigned_agent": "explorer",
				"required_tools": ["read"],
				"affected_paths": ["calc.go"]
			},
			{
				"id": "T2",
				"title": "Implement Multiply",
				"description": "Append Multiply function to calc.go",
				"assigned_agent": "implementer",
				"dependencies": ["T1"],
				"required_tools": ["write"],
				"affected_paths": ["calc.go"],
				"verification": "echo verify-calc-ok"
			}
		]
	}`

	mockModel := mock.New("mock-orch", []core.ModelInfo{
		mock.Model("mock-orch", "planner-model", 0.9, 100000, true),
	}, []mock.Script{
		// 1. Planner call
		{Resp: &core.ChatResponse{Content: planJSON}},
		// 2. Explorer T1 call (reads calc.go)
		{Resp: &core.ChatResponse{
			Content:   "reading calc.go",
			ToolCalls: []core.ToolCall{{ID: "c1", Name: "read", Arguments: `{"path":"calc.go"}`}},
		}},
		{Resp: &core.ChatResponse{Content: "Found Add function in calc.go"}},
		// 3. Implementer T2 call (writes Multiply to calc.go)
		{Resp: &core.ChatResponse{
			Content: "writing Multiply",
			ToolCalls: []core.ToolCall{{ID: "c2", Name: "write", Arguments: `{"path":"calc.go","content":"package calc\nfunc Add(a, b int) int { return a + b }\nfunc Multiply(a, b int) int { return a * b }\n"}`}},
		}},
		{Resp: &core.ChatResponse{Content: "Added Multiply to calc.go"}},
	})

	reg := router.NewRegistry()
	reg.Register(mockModel)
	ms, _ := mockModel.ListModels(context.Background())
	var es []router.Entry
	for _, m := range ms {
		es = append(es, router.Entry{Model: m, Backend: mockModel})
	}
	reg.SetModels(es)
	r := router.New(reg)
	r.Backoff = 0

	orch := NewOrchestrator(r, tools)
	sess := NewSession("s-orch-1", "Add Multiply to calc.go and verify", tools)

	finalReport, err := orch.Execute(context.Background(), sess, router.DefaultPrefs())
	if err != nil {
		t.Fatalf("orchestrator execution failed: %v", err)
	}

	if !strings.Contains(finalReport, "Verified Complete") {
		t.Errorf("final report missing completion status: %s", finalReport)
	}

	// Verify file was modified on disk
	content, err := os.ReadFile(filepath.Join(dir, "calc.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "Multiply") {
		t.Errorf("calc.go does not contain Multiply: %s", string(content))
	}

	// Verify task graph state
	if !orch.Graph.IsComplete() {
		t.Error("task graph must be complete")
	}
	t1, _ := orch.Graph.GetTask("T1")
	t2, _ := orch.Graph.GetTask("T2")
	if t1.Status != TaskCompleted || t2.Status != TaskCompleted {
		t.Errorf("task statuses: T1=%s, T2=%s", t1.Status, t2.Status)
	}

	// Verify trace has events
	traceEvents := orch.Trace.Events()
	if len(traceEvents) == 0 {
		t.Error("execution trace must not be empty")
	}

	// Verify session recorded verifications
	if len(sess.Session.VerificationHistory) == 0 {
		t.Error("verification history should be recorded in session")
	}
}

// TestOrchestrationFailedVerificationAndRepair tests:
// Verification fails -> diagnosis -> repair task generated -> repair executes -> re-verification passes.
func TestOrchestrationFailedVerificationAndRepair(t *testing.T) {
	dir := t.TempDir()
	tools := ct.NewRegistry(dir, ct.Policy{AutoApprove: true})

	planJSON := `{
		"goal": "Create feature with test",
		"tasks": [
			{
				"id": "T1",
				"title": "Implement feature",
				"description": "Write buggy code first",
				"assigned_agent": "implementer",
				"verification": "bash -c 'if [ -f fixed.txt ]; then echo pass; else echo fail >&2; exit 1; fi'"
			}
		]
	}`

	mockModel := mock.New("mock-repair", []core.ModelInfo{
		mock.Model("mock-repair", "repair-model", 0.9, 100000, true),
	}, []mock.Script{
		// 1. Planner
		{Resp: &core.ChatResponse{Content: planJSON}},
		// 2. Initial implementation of T1 (doesn't create fixed.txt)
		{Resp: &core.ChatResponse{Content: "Initial implementation done without fixed.txt"}},
		// 3. Repair task T1-R2 executes (creates fixed.txt to satisfy verification!)
		{Resp: &core.ChatResponse{
			Content: "Repairing by creating fixed.txt",
			ToolCalls: []core.ToolCall{
				{ID: "c1", Name: "write", Arguments: `{"path":"fixed.txt","content":"fixed"}`},
			},
		}},
		{Resp: &core.ChatResponse{Content: "Created fixed.txt successfully."}},
	})

	reg := router.NewRegistry()
	reg.Register(mockModel)
	ms, _ := mockModel.ListModels(context.Background())
	var es []router.Entry
	for _, m := range ms {
		es = append(es, router.Entry{Model: m, Backend: mockModel})
	}
	reg.SetModels(es)
	r := router.New(reg)
	r.Backoff = 0

	orch := NewOrchestrator(r, tools)
	sess := NewSession("s-repair", "Create feature with test", tools)

	finalReport, err := orch.Execute(context.Background(), sess, router.DefaultPrefs())
	if err != nil {
		t.Fatalf("orchestrator execution failed: %v", err)
	}

	// Verify repair task was created and passed
	repairTask, exists := orch.Graph.GetTask("T1-R2")
	if !exists {
		t.Fatal("expected repair task T1-R2 in graph")
	}
	if repairTask.Status != TaskCompleted {
		t.Errorf("repair task status = %s, want completed", repairTask.Status)
	}

	// Verify file fixed.txt was written by the repair task
	if _, err := os.Stat(filepath.Join(dir, "fixed.txt")); err != nil {
		t.Errorf("fixed.txt was not created by repair task: %v", err)
	}

	if !strings.Contains(finalReport, "Verified Complete") {
		t.Errorf("expected final report to reflect verified complete after repair: %s", finalReport)
	}
}

// TestOrchestrationModelFallback tests that a model failure mid-task
// triggers automatic fallback to a secondary model and preserves graph and session state.
func TestOrchestrationModelFallback(t *testing.T) {
	dir := t.TempDir()
	tools := ct.NewRegistry(dir, ct.Policy{AutoApprove: true})

	planJSON := `{
		"goal": "Test fallback",
		"tasks": [
			{"id": "T1", "title": "Direct task", "assigned_agent": "orchestrator", "description": "do thing"}
		]
	}`

	// Primary backend fails with rate limit during direct task
	primary := mock.New("primary", []core.ModelInfo{
		mock.Model("primary", "model-a", 0.9, 100000, true),
	}, []mock.Script{
		// 1. Planner succeeds on model-a
		{Resp: &core.ChatResponse{Content: planJSON}},
		// 2. Direct task execution fails with 429 Rate Limited
		{Err: &core.ProviderError{Code: core.ErrRateLimited, Message: "rate limit exceeded"}},
	})

	// Secondary backend takes over and succeeds
	secondary := mock.New("secondary", []core.ModelInfo{
		mock.Model("secondary", "model-b", 0.8, 100000, true),
	}, []mock.Script{
		// Finishes direct task
		{Resp: &core.ChatResponse{Content: "Completed successfully on secondary model"}},
	})

	reg := router.NewRegistry()
	reg.Register(primary)
	reg.Register(secondary)

	var es []router.Entry
	pms, _ := primary.ListModels(context.Background())
	for _, m := range pms {
		es = append(es, router.Entry{Model: m, Backend: primary})
	}
	sms, _ := secondary.ListModels(context.Background())
	for _, m := range sms {
		es = append(es, router.Entry{Model: m, Backend: secondary})
	}
	reg.SetModels(es)

	r := router.New(reg)
	r.Backoff = 0

	var switches []router.SwitchInfo
	r.OnSwitch = func(si router.SwitchInfo) {
		switches = append(switches, si)
	}

	orch := NewOrchestrator(r, tools)
	sess := NewSession("s-fallback", "Test fallback", tools)

	finalReport, err := orch.Execute(context.Background(), sess, router.DefaultPrefs())
	if err != nil {
		t.Fatalf("orchestrator execution failed: %v", err)
	}

	if len(switches) == 0 {
		t.Fatal("expected model switch during fallback")
	}
	if switches[0].To != "secondary/model-b" {
		t.Errorf("switched to %q, want secondary/model-b", switches[0].To)
	}

	if !strings.Contains(finalReport, "Verified Complete") {
		t.Errorf("expected final report to be complete, got: %s", finalReport)
	}
}

func TestOrchestratorConversationalFastPath(t *testing.T) {
	// Verify isConversational matching
	for _, greeting := range []string{"hi", "hello", "hey", "yo", "sup", "howdy", "who are you"} {
		if !isConversational(greeting) {
			t.Errorf("expected isConversational(%q) = true", greeting)
		}
	}
	for _, task := range []string{"fix the typo in auth.go", "create dir foo", "can u make a new dir here called tiki2"} {
		if isConversational(task) {
			t.Errorf("expected isConversational(%q) = false", task)
		}
	}

	dir := t.TempDir()
	tools := ct.NewRegistry(dir, ct.Policy{AutoApprove: true})

	mockModel := mock.New("mock-chat", []core.ModelInfo{
		mock.Model("mock-chat", "fast-chat", 0.9, 100000, true),
	}, []mock.Script{
		{Resp: &core.ChatResponse{Content: "Hello! How can I help you today?"}},
	})

	reg := router.NewRegistry()
	reg.Register(mockModel)
	ms, _ := mockModel.ListModels(context.Background())
	var es []router.Entry
	for _, m := range ms {
		es = append(es, router.Entry{Model: m, Backend: mockModel})
	}
	reg.SetModels(es)
	r := router.New(reg)

	var emittedEvents []EventType
	orch := NewOrchestrator(r, tools)
	orch.Emit = func(ev Event) {
		emittedEvents = append(emittedEvents, ev.Type)
	}

	sess := NewSession("s-hello", "hi", tools)
	resp, err := orch.Execute(context.Background(), sess, router.DefaultPrefs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "Hello! How can I help you today?" {
		t.Errorf("unexpected response: %q", resp)
	}

	// Must NOT emit EvGraph (no DAG planning) or EvVerifyStart (no verification)
	for _, ev := range emittedEvents {
		if ev == EvGraph {
			t.Error("conversational turn must not plan task graph")
		}
		if ev == EvVerifyStart {
			t.Error("conversational turn must not run verification")
		}
	}
}

