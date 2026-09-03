package router

import (
	"context"
	"strings"
	"testing"
	"time"

	"zeuf/internal/core"
	"zeuf/internal/providers/mock"
)

func entries(t *testing.T) []Entry {
	t.Helper()
	ma := mock.New("backend-a", []core.ModelInfo{mock.Model("pa", "a1", 0.9, 200000, true)}, nil)
	mb := mock.New("backend-b", []core.ModelInfo{mock.Model("pb", "b1", 0.5, 200000, true)}, nil)
	return []Entry{
		{Model: mock.Model("pa", "a1", 0.9, 200000, true), Backend: ma},
		{Model: mock.Model("pb", "b1", 0.5, 200000, true), Backend: mb},
	}
}

func TestRankSkipsDeadBackends(t *testing.T) {
	reg := NewRegistry()
	es := entries(t)
	dead := mock.Model("pg", "g1", 0.99, 1000000, true)
	dead.Availability = core.AvailAuthError
	dead.LastError = "no gemini auth"
	mg := mock.New("backend-g", nil, nil)
	es = append(es, Entry{Model: dead, Backend: mg})
	reg.SetModels(es)
	ranked := New(reg).Ranked(TaskReq{}, DefaultPrefs())
	for _, s := range ranked {
		if s.Entry.Model.ID == "g1" {
			t.Errorf("logged-out model must not be ranked while usable ones exist: %+v", ranked)
		}
	}
	if len(ranked) != 2 {
		t.Errorf("want the 2 usable models, got %d", len(ranked))
	}
}

func TestRankLastResortWhenAllDead(t *testing.T) {
	reg := NewRegistry()
	es := entries(t)
	for i := range es {
		es[i].Model.Availability = core.AvailQuotaOut
	}
	reg.SetModels(es)
	ranked := New(reg).Ranked(TaskReq{}, DefaultPrefs())
	if len(ranked) == 0 {
		t.Error("all-dead pool must still attempt (clear error beats silence)")
	}
}

func TestRankPrefersCapableOverFirst(t *testing.T) {
	reg := NewRegistry()
	reg.SetModels(entries(t))
	r := New(reg)
	task := TaskReq{NeedTools: true, PreferCoding: true}
	ranked := r.Ranked(task, DefaultPrefs())
	if len(ranked) != 2 {
		t.Fatalf("ranked %d, want 2", len(ranked))
	}
	if ranked[0].Entry.Model.ID != "a1" {
		t.Errorf("best = %s, want a1 (higher coding score)", ranked[0].Entry.Model.ID)
	}
}

func TestRankFilters(t *testing.T) {
	reg := NewRegistry()
	es := entries(t)
	es[0].Model.Caps.ContextLength = 8000 // too small
	reg.SetModels(es)
	ranked := New(reg).Ranked(TaskReq{MinContext: 100000}, DefaultPrefs())
	if len(ranked) != 1 || ranked[0].Entry.Model.ID != "b1" {
		t.Errorf("context filter wrong: %+v", ranked)
	}

	reg2 := NewRegistry()
	reg2.SetModels(entries(t))
	prefs := DefaultPrefs()
	prefs.DisabledProviders = []string{"backend-a"}
	ranked = New(reg2).Ranked(TaskReq{}, prefs)
	if len(ranked) != 1 || ranked[0].Entry.Backend.Name() != "backend-b" {
		t.Errorf("disabled filter wrong: %+v", ranked)
	}
}

func TestPinnedModelWins(t *testing.T) {
	reg := NewRegistry()
	reg.SetModels(entries(t))
	prefs := DefaultPrefs()
	prefs.PinnedModel = "pb/b1"
	ranked := New(reg).Ranked(TaskReq{PreferCoding: true}, prefs)
	if len(ranked) != 1 || ranked[0].Entry.Model.ID != "b1" {
		t.Errorf("pin should win: %+v", ranked)
	}
}

// TestExecuteFallback is the milestone test: Provider A dies with a
// quota error mid-task, Provider B continues the SAME session.
func TestExecuteFallback(t *testing.T) {
	ctx := context.Background()
	quotaErr := &core.ProviderError{Code: core.ErrQuotaOut, Provider: "backend-a", Message: "quota exhausted"}
	ma := mock.New("backend-a",
		[]core.ModelInfo{mock.Model("pa", "a1", 0.95, 200000, true)},
		[]mock.Script{{Err: quotaErr}})
	mb := mock.New("backend-b",
		[]core.ModelInfo{mock.Model("pb", "b1", 0.4, 200000, true)},
		[]mock.Script{{Resp: &core.ChatResponse{Content: "continued and finished"}}})

	reg := NewRegistry()
	reg.Register(ma)
	reg.Register(mb)
	reg.SetModels([]Entry{
		{Model: mock.Model("pa", "a1", 0.95, 200000, true), Backend: ma},
		{Model: mock.Model("pb", "b1", 0.4, 200000, true), Backend: mb},
	})
	r := New(reg)
	r.Backoff = 0
	var switches []SwitchInfo
	r.OnSwitch = func(s SwitchInfo) { switches = append(switches, s) }

	// The session going in: full history a real task would carry.
	req := core.ChatRequest{Messages: []core.Message{
		{Role: core.RoleSystem, Content: "sys"},
		{Role: core.RoleUser, Content: "fix the bug"},
		{Role: core.RoleAssistant, Content: "plan: repro then fix"},
		{Role: core.RoleTool, Name: "read", Content: "file data"},
	}}
	prefs := DefaultPrefs()
	resp, entry, err := r.Do(ctx, req, TaskReq{PreferCoding: true}, prefs,
		func(e Entry, rq core.ChatRequest) (*core.ChatResponse, error) {
			return e.Backend.Chat(ctx, rq)
		})
	if err != nil {
		t.Fatalf("fallback failed: %v", err)
	}
	if resp.Content != "continued and finished" {
		t.Errorf("content = %q", resp.Content)
	}
	if entry.Backend.Name() != "backend-b" {
		t.Errorf("winner = %s, want backend-b", entry.Backend.Name())
	}
	if len(switches) != 1 || switches[0].To != "pb/b1" {
		t.Errorf("switch notice wrong: %+v", switches)
	}
	// Session preservation: B received the SAME history, not a fresh prompt.
	if len(mb.Requests) != 1 {
		t.Fatalf("backend-b got %d requests", len(mb.Requests))
	}
	gotMsgs := mb.Requests[0].Messages
	if len(gotMsgs) != len(req.Messages) {
		t.Fatalf("backend-b got %d messages, want %d (full history)", len(gotMsgs), len(req.Messages))
	}
	for i := range gotMsgs {
		if gotMsgs[i].Content != req.Messages[i].Content {
			t.Errorf("message %d altered across fallback: %q", i, gotMsgs[i].Content)
		}
	}
	// A is cooling down with quota state visible.
	st := reg.Tracker().State("backend-a/a1")
	if st == nil || st.Availability != core.AvailQuotaOut {
		t.Errorf("model A should be parked as quota_exhausted: %+v", st)
	}
}

func TestFallbackDisabledTriesOnce(t *testing.T) {
	ctx := context.Background()
	ma := mock.New("backend-a", []core.ModelInfo{mock.Model("pa", "a1", 0.9, 0, true)},
		[]mock.Script{{Err: &core.ProviderError{Code: core.ErrRateLimited, Message: "slow down"}}})
	mb := mock.New("backend-b", []core.ModelInfo{mock.Model("pb", "b1", 0.1, 0, true)},
		[]mock.Script{{Resp: &core.ChatResponse{Content: "b wins"}}})
	reg := NewRegistry()
	reg.Register(ma)
	reg.Register(mb)
	reg.SetModels([]Entry{
		{Model: mock.Model("pa", "a1", 0.9, 0, true), Backend: ma},
		{Model: mock.Model("pb", "b1", 0.1, 0, true), Backend: mb},
	})
	r := New(reg)
	r.Backoff = 0
	prefs := DefaultPrefs()
	prefs.FallbackEnabled = false
	_, _, err := r.Do(ctx, core.ChatRequest{}, TaskReq{}, prefs,
		func(e Entry, rq core.ChatRequest) (*core.ChatResponse, error) {
			return e.Backend.Chat(ctx, rq)
		})
	if err == nil {
		t.Fatal("expected the rate-limit error to surface")
	}
	if mb.Calls() != 0 {
		t.Error("fallback disabled but backend-b was tried")
	}
}

// TestAuthSkipsBackendSiblings: one auth failure poisons the whole backend
// for the turn — its other models are skipped, not burned one by one.
func TestAuthSkipsBackendSiblings(t *testing.T) {
	ctx := context.Background()
	authErr := &core.ProviderError{Code: core.ErrAuth, Provider: "backend-a", Message: "bad login"}
	ma := mock.New("backend-a",
		[]core.ModelInfo{mock.Model("pa", "a1", 0.9, 0, true), mock.Model("pa", "a2", 0.89, 0, true)},
		[]mock.Script{{Err: authErr}})
	mb := mock.New("backend-b",
		[]core.ModelInfo{mock.Model("pb", "b1", 0.1, 0, true)},
		[]mock.Script{{Resp: &core.ChatResponse{Content: "b wins"}}})
	reg := NewRegistry()
	reg.Register(ma)
	reg.Register(mb)
	reg.SetModels([]Entry{
		{Model: mock.Model("pa", "a1", 0.9, 0, true), Backend: ma},
		{Model: mock.Model("pa", "a2", 0.89, 0, true), Backend: ma},
		{Model: mock.Model("pb", "b1", 0.1, 0, true), Backend: mb},
	})
	r := New(reg)
	r.Backoff = 0
	resp, _, err := r.Do(ctx, core.ChatRequest{}, TaskReq{}, DefaultPrefs(),
		func(e Entry, rq core.ChatRequest) (*core.ChatResponse, error) {
			return e.Backend.Chat(ctx, rq)
		})
	if err != nil {
		t.Fatalf("fallback failed: %v", err)
	}
	if resp.Content != "b wins" {
		t.Errorf("content = %q", resp.Content)
	}
	if ma.Calls() != 1 {
		t.Errorf("backend-a tried %d times, want 1 (siblings skipped after auth failure)", ma.Calls())
	}
}

// TestFailureTrail: a dead turn names every model attempted.
func TestFailureTrail(t *testing.T) {
	ctx := context.Background()
	ma := mock.New("backend-a", []core.ModelInfo{mock.Model("pa", "a1", 0.9, 0, true)},
		[]mock.Script{{Err: &core.ProviderError{Code: core.ErrOverloaded, Message: "down"}}})
	mb := mock.New("backend-b", []core.ModelInfo{mock.Model("pb", "b1", 0.1, 0, true)},
		[]mock.Script{{Err: &core.ProviderError{Code: core.ErrOverloaded, Message: "also down"}}})
	reg := NewRegistry()
	reg.Register(ma)
	reg.Register(mb)
	reg.SetModels([]Entry{
		{Model: mock.Model("pa", "a1", 0.9, 0, true), Backend: ma},
		{Model: mock.Model("pb", "b1", 0.1, 0, true), Backend: mb},
	})
	r := New(reg)
	r.Backoff = 0
	_, _, err := r.Do(ctx, core.ChatRequest{}, TaskReq{}, DefaultPrefs(),
		func(e Entry, rq core.ChatRequest) (*core.ChatResponse, error) {
			return e.Backend.Chat(ctx, rq)
		})
	if err == nil {
		t.Fatal("expected failure")
	}
	msg := err.Error()
	if !strings.Contains(msg, "pa/a1") || !strings.Contains(msg, "pb/b1") || !strings.Contains(msg, "tried:") {
		t.Errorf("trail missing from error: %q", msg)
	}
}

// TestDiversifyAfterTransientFailure: when backend-a overloads, the next
// attempt prefers backend-b even though backend-a's second model ranks
// higher — one flaky family must not eat the attempt budget.
func TestDiversifyAfterTransientFailure(t *testing.T) {
	ctx := context.Background()
	over := &core.ProviderError{Code: core.ErrOverloaded, Message: "boom"}
	ma := mock.New("backend-a",
		[]core.ModelInfo{mock.Model("pa", "a1", 0.9, 0, true), mock.Model("pa", "a2", 0.89, 0, true)},
		[]mock.Script{{Err: over}})
	mb := mock.New("backend-b",
		[]core.ModelInfo{mock.Model("pb", "b1", 0.1, 0, true)},
		[]mock.Script{{Resp: &core.ChatResponse{Content: "b wins"}}})
	reg := NewRegistry()
	reg.Register(ma)
	reg.Register(mb)
	reg.SetModels([]Entry{
		{Model: mock.Model("pa", "a1", 0.9, 0, true), Backend: ma},
		{Model: mock.Model("pa", "a2", 0.89, 0, true), Backend: ma},
		{Model: mock.Model("pb", "b1", 0.1, 0, true), Backend: mb},
	})
	r := New(reg)
	r.Backoff = 0
	var order []string
	resp, _, err := r.Do(ctx, core.ChatRequest{}, TaskReq{}, DefaultPrefs(),
		func(e Entry, rq core.ChatRequest) (*core.ChatResponse, error) {
			order = append(order, e.Model.FullID())
			return e.Backend.Chat(ctx, rq)
		})
	if err != nil {
		t.Fatalf("fallback failed: %v", err)
	}
	if resp.Content != "b wins" {
		t.Errorf("content = %q", resp.Content)
	}
	if len(order) != 2 || order[0] != "pa/a1" || order[1] != "pb/b1" {
		t.Errorf("attempt order = %v, want [pa/a1 pb/b1]", order)
	}
}

func TestCooldownSkipsModel(t *testing.T) {
	reg := NewRegistry()
	es := entries(t)
	reg.SetModels(es)
	reg.Tracker().RecordFailure("backend-a/a1", &core.ProviderError{Code: core.ErrRateLimited, Message: "slow"})
	ranked := New(reg).Ranked(TaskReq{}, DefaultPrefs())
	for _, s := range ranked {
		if s.Entry.Model.ID == "a1" {
			t.Error("cooling-down model should be filtered")
		}
	}
}

func TestHealthBackoffGrows(t *testing.T) {
	tr := NewTracker()
	tr.RecordFailure("k", &core.ProviderError{Code: core.ErrQuotaOut, Message: "q"})
	first := tr.State("k").CooldownUntil
	tr.RecordFailure("k", &core.ProviderError{Code: core.ErrQuotaOut, Message: "q"})
	if !tr.State("k").CooldownUntil.After(first) {
		t.Error("cooldown should grow exponentially")
	}
	tr.RecordSuccess("k", time.Second)
	if st := tr.State("k"); st.ConsecutiveFails != 0 {
		t.Error("success should clear the streak")
	}
}

func TestFreeOnly(t *testing.T) {
	free := mock.Model("pk", "f1", 0.5, 0, true)
	free.IsFree = true
	paid := mock.Model("pk", "p1", 0.9, 0, true)
	paid.IsFree = false
	paid.CostKnown = true
	unknown := mock.Model("pk", "u1", 0.9, 0, true)
	mb := mock.New("pk", nil, nil)
	es := []Entry{{Model: free, Backend: mb}, {Model: paid, Backend: mb}, {Model: unknown, Backend: mb}}
	got := FreeOnly(es)
	if len(got) != 1 || got[0].Model.ID != "f1" {
		t.Errorf("FreeOnly kept %+v, want only f1", got)
	}
}
