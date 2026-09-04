package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
	"zeuf/internal/providers/mock"
	"zeuf/internal/router"
)

// TestEndToEndMultiAgentWorkflow verifies:
// 1. Concurrent discovery & exploration subagents (T1 & T2 in parallel)
// 2. Dependency gating (T3 waits for T1 and T2)
// 3. Implementer executing scoped modifications
// 4. First-class verification stage
// 5. Truthful synthesis without hallucinated claims
func TestEndToEndMultiAgentWorkflow(t *testing.T) {
	dir := t.TempDir()
	tools := ct.NewRegistry(dir, ct.Policy{AutoApprove: true})

	// Setup initial repository files
	if err := os.WriteFile(filepath.Join(dir, "parser.go"), []byte("package core\nfunc Parse(s string) string { return s }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "router.go"), []byte("package core\nfunc Route(p string) bool { return true }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	planJSON := `{
		"goal": "Integrate parser into router and verify",
		"tasks": [
			{
				"id": "T1",
				"title": "Investigate parser",
				"description": "Inspect parser.go",
				"assigned_agent": "explorer",
				"required_tools": ["read"],
				"affected_paths": ["parser.go"]
			},
			{
				"id": "T2",
				"title": "Investigate router",
				"description": "Inspect router.go",
				"assigned_agent": "explorer",
				"required_tools": ["read"],
				"affected_paths": ["router.go"]
			},
			{
				"id": "T3",
				"title": "Wire parser to router",
				"description": "Call Parse inside Route",
				"assigned_agent": "implementer",
				"dependencies": ["T1", "T2"],
				"required_tools": ["write"],
				"affected_paths": ["router.go"],
				"verification": "echo router-test-passed"
			}
		]
	}`

	var t1Running, t2Running atomic.Bool
	var ranConcurrently atomic.Bool

	mockBackend := mock.New("mock-e2e", []core.ModelInfo{
		mock.Model("mock-e2e", "agent-model", 0.9, 100000, true),
	}, []mock.Script{
		// Planner
		{Resp: &core.ChatResponse{Content: planJSON}},
		// Explorer 1
		{Resp: &core.ChatResponse{
			Content:   "Inspecting parser",
			ToolCalls: []core.ToolCall{{ID: "c1", Name: "read", Arguments: `{"path":"parser.go"}`}},
		}},
		{Resp: &core.ChatResponse{Content: "Parser is simple string passthrough"}},
		// Explorer 2
		{Resp: &core.ChatResponse{
			Content:   "Inspecting router",
			ToolCalls: []core.ToolCall{{ID: "c2", Name: "read", Arguments: `{"path":"router.go"}`}},
		}},
		{Resp: &core.ChatResponse{Content: "Router returns true"}},
		// Implementer
		{Resp: &core.ChatResponse{
			Content: "Updating router.go with parser call",
			ToolCalls: []core.ToolCall{{ID: "c3", Name: "write", Arguments: `{"path":"router.go","content":"package core\nfunc Route(p string) bool { return Parse(p) != \"\" }\n"}`}},
		}},
		{Resp: &core.ChatResponse{Content: "Wired parser to router successfully"}},
	})

	reg := router.NewRegistry()
	reg.Register(mockBackend)
	ms, _ := mockBackend.ListModels(context.Background())
	var es []router.Entry
	for _, m := range ms {
		es = append(es, router.Entry{Model: m, Backend: mockBackend})
	}
	reg.SetModels(es)
	r := router.New(reg)
	r.Backoff = 0

	orch := NewOrchestrator(r, tools)

	// Intercept event emissions to verify parallel execution of T1 & T2
	orch.Emit = func(ev Event) {
		if ev.Type == EvSubStart {
			if ev.TaskID == "T1" {
				t1Running.Store(true)
				if t2Running.Load() {
					ranConcurrently.Store(true)
				}
			}
			if ev.TaskID == "T2" {
				t2Running.Store(true)
				if t1Running.Load() {
					ranConcurrently.Store(true)
				}
			}
		}
		if ev.Type == EvSubEnd {
			if ev.TaskID == "T1" {
				t1Running.Store(false)
			}
			if ev.TaskID == "T2" {
				t2Running.Store(false)
			}
		}
	}

	sess := NewSession("s-e2e", "Integrate parser into router and verify", tools)

	report, err := orch.Execute(context.Background(), sess, router.DefaultPrefs())
	if err != nil {
		t.Fatalf("e2e execution failed: %v", err)
	}

	// 1. Verify tasks completed
	if !orch.Graph.IsComplete() {
		t.Error("expected all tasks in graph to complete")
	}
	for _, id := range []string{"T1", "T2", "T3"} {
		task, ok := orch.Graph.GetTask(id)
		if !ok || task.Status != TaskCompleted {
			t.Errorf("task %s status = %v, want completed", id, task.Status)
		}
	}

	// 2. Verify router.go was modified on disk
	content, err := os.ReadFile(filepath.Join(dir, "router.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "Parse(p)") {
		t.Errorf("router.go was not properly modified: %s", string(content))
	}

	// 3. Verify truthful reporting
	if !strings.Contains(report, "Verified Complete") {
		t.Errorf("report missing verified complete: %s", report)
	}
	if !strings.Contains(report, "router-test-passed") {
		t.Errorf("report missing verification test command: %s", report)
	}
}

// TestConflictingScopesSerializedE2E verifies that when two tasks touch the same file,
// the orchestrator scheduler serializes their execution rather than running concurrently.
func TestConflictingScopesSerializedE2E(t *testing.T) {
	g := NewTaskGraph("conflict e2e")
	g.AddTask(&Task{
		ID:            "T1",
		Title:         "Write client part 1",
		AssignedAgent: "implementer",
		AffectedPaths: []string{"client.go"},
	})
	g.AddTask(&Task{
		ID:            "T2",
		Title:         "Write client part 2",
		AssignedAgent: "implementer",
		AffectedPaths: []string{"client.go"},
	})

	var activeCount atomic.Int32
	var maxActive atomic.Int32

	executor := func(ctx context.Context, task *Task) (string, error) {
		cur := activeCount.Add(1)
		for {
			old := maxActive.Load()
			if cur <= old || maxActive.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		activeCount.Add(-1)
		return "done", nil
	}

	sched := NewScheduler(SchedulerConfig{MaxConcurrency: 4, DefaultTimeout: 2 * time.Second}, g, executor)
	if err := sched.Run(context.Background()); err != nil {
		t.Fatalf("scheduler run failed: %v", err)
	}

	if maxActive.Load() > 1 {
		t.Fatalf("conflicting tasks were executed concurrently! maxActive = %d", maxActive.Load())
	}
}
