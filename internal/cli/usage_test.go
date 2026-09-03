package cli

import (
	"testing"

	"zeuf/internal/agent"
	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
	"zeuf/internal/providers/mock"
	"zeuf/internal/router"
)

func TestFmtK(t *testing.T) {
	cases := map[int64]string{
		0: "0", 999: "999", 1000: "1k", 2000: "2k",
		12400: "12.4k", 200000: "200k", 1048576: "1.0M", 3400000: "3.4M",
	}
	for n, want := range cases {
		if got := fmtK(n); got != want {
			t.Errorf("fmtK(%d) = %q, want %q", n, got, want)
		}
	}
}

func usageTestReg(t *testing.T) *router.Registry {
	t.Helper()
	mb := mock.New("opencode", nil, nil)
	reg := router.NewRegistry()
	reg.Register(mb)
	reg.SetModels([]router.Entry{{
		Model:   mock.Model("opencode", "m1", 0.9, 200000, true),
		Backend: mb,
	}})
	return reg
}

func TestUsageEventKnownWindow(t *testing.T) {
	reg := usageTestReg(t)
	tools := ct.NewRegistry(t.TempDir(), ct.Policy{AutoApprove: true})
	sess := agent.NewSession("s", "hi", tools)
	sess.AppendUser("hi")
	sess.AddUsage(core.Usage{Input: 12000, Output: 400})
	ev := usageEvent(reg, sess, "opencode/m1")
	if ev.Kind != "usage" || ev.Text != "12.4k/200k" || ev.Detail != "6%" {
		t.Errorf("usage event = %+v", ev)
	}
}

func TestUsageEventUnknown(t *testing.T) {
	reg := usageTestReg(t)
	tools := ct.NewRegistry(t.TempDir(), ct.Policy{AutoApprove: true})
	sess := agent.NewSession("s", "hi", tools)
	// No usage yet: silent.
	if ev := usageEvent(reg, sess, "opencode/m1"); ev.Text != "" {
		t.Errorf("empty totals must stay silent: %+v", ev)
	}
	// Unknown model: honest partial.
	sess.AddUsage(core.Usage{Input: 500, Output: 0})
	if ev := usageEvent(reg, sess, "nope/unknown"); ev.Text != "500/?" || ev.Detail != "" {
		t.Errorf("unknown window wrong: %+v", ev)
	}
}
