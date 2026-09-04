package agent

import (
	"context"
	"strings"
	"testing"

	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
	"zeuf/internal/providers/mock"
	"zeuf/internal/router"
)

func TestSpecialistToolScoping(t *testing.T) {
	dir := t.TempDir()
	tools := ct.NewRegistry(dir, ct.Policy{AutoApprove: true})

	// Explorer role should filter out mutating tools
	explorerTools := ScopeRegistry(tools, AllowedToolsForRole(RoleExplorer), RoleExplorer)
	for _, name := range explorerTools.Names() {
		if name == "write" || name == "edit" {
			t.Errorf("explorer must not have %q tool", name)
		}
	}

	// Depth limit: no specialist role should EVER have the delegate tool
	for _, role := range []SpecialistRole{RoleExplorer, RoleImplementer, RoleTester, RoleReviewer, RoleResearcher} {
		scoped := ScopeRegistry(tools, AllowedToolsForRole(role), role)
		for _, name := range scoped.Names() {
			if name == "delegate" {
				t.Fatalf("specialist role %q must never have delegate tool (depth limit 1)", role)
			}
		}
	}
}

func TestSpecialistExecutionWithMock(t *testing.T) {
	dir := t.TempDir()
	tools := ct.NewRegistry(dir, ct.Policy{AutoApprove: true})

	mockAdapter := mock.New("mock-specialist", []core.ModelInfo{
		mock.Model("mock-specialist", "test-model", 0.9, 100000, true),
	}, []mock.Script{
		{Resp: &core.ChatResponse{
			Content: "Investigating files",
			ToolCalls: []core.ToolCall{
				{ID: "c1", Name: "glob", Arguments: `{"pattern":"*"}`},
			},
		}},
		{Resp: &core.ChatResponse{
			Content: "Found 0 files in workspace. Clean state.",
		}},
	})

	reg := router.NewRegistry()
	reg.Register(mockAdapter)
	ms, _ := mockAdapter.ListModels(context.Background())
	var es []router.Entry
	for _, m := range ms {
		es = append(es, router.Entry{Model: m, Backend: mockAdapter})
	}
	reg.SetModels(es)
	r := router.New(reg)
	r.Backoff = 0

	brief := DelegationBrief{
		TaskID:    "T1",
		Role:      RoleExplorer,
		Objective: "Check repo files",
	}

	res, err := RunSpecialistTurn(context.Background(), brief, r, tools, router.DefaultPrefs(), nil)
	if err != nil {
		t.Fatalf("specialist turn failed: %v", err)
	}

	if !strings.Contains(res.Summary, "Clean state") {
		t.Errorf("summary = %q, want 'Clean state'", res.Summary)
	}
	if res.ToolCallsCount != 1 {
		t.Errorf("tool calls count = %d, want 1", res.ToolCallsCount)
	}
}

func TestExplorerCannotMutate(t *testing.T) {
	dir := t.TempDir()
	tools := ct.NewRegistry(dir, ct.Policy{AutoApprove: true})

	// Script mock to attempt calling 'write'
	mockAdapter := mock.New("mock-explorer", []core.ModelInfo{
		mock.Model("mock-explorer", "test-model", 0.9, 100000, true),
	}, []mock.Script{
		{Resp: &core.ChatResponse{
			Content: "Trying to write",
			ToolCalls: []core.ToolCall{
				{ID: "c1", Name: "write", Arguments: `{"path":"bad.txt","content":"bad"}`},
			},
		}},
		{Resp: &core.ChatResponse{
			Content: "Write failed as expected.",
		}},
	})

	reg := router.NewRegistry()
	reg.Register(mockAdapter)
	ms, _ := mockAdapter.ListModels(context.Background())
	var es []router.Entry
	for _, m := range ms {
		es = append(es, router.Entry{Model: m, Backend: mockAdapter})
	}
	reg.SetModels(es)
	r := router.New(reg)
	r.Backoff = 0

	brief := DelegationBrief{
		TaskID:    "T1",
		Role:      RoleExplorer,
		Objective: "Read only",
	}

	res, err := RunSpecialistTurn(context.Background(), brief, r, tools, router.DefaultPrefs(), nil)
	if err != nil {
		t.Fatalf("specialist turn failed: %v", err)
	}

	if len(res.FilesTouched) > 0 {
		t.Errorf("explorer must not touch files, touched: %v", res.FilesTouched)
	}
}
