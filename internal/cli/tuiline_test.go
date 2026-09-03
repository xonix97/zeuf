package cli

import (
	"context"
	"testing"
	"time"

	"zeuf/internal/agent"
	ct "zeuf/internal/core/tools"
	"zeuf/internal/providers/mock"
	"zeuf/internal/router"
	"zeuf/internal/tui"
)

func TestHandleTUILineModels(t *testing.T) {
	ctx := context.Background()
	tools := ct.NewRegistry(t.TempDir(), ct.Policy{AutoApprove: true})
	reg := router.NewRegistry()
	mb := mock.New("opencode", nil, nil)
	reg.Register(mb)
	reg.SetModels([]router.Entry{{Model: mock.Model("opencode", "m1", 0.9, 200000, true), Backend: mb}})
	r := router.New(reg)
	r.Backoff = 0
	prefs := router.DefaultPrefs()
	ag := agent.New(r, tools)
	sess := agent.NewSession("s", "", tools)
	events := make(chan tui.Event, 64)
	if quit := handleTUILine(ctx, "/models", &prefs, reg, r, ag, sess, events, nil, nil); quit {
		t.Fatal("/models should not quit")
	}
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Kind == "picker" {
				if len(ev.Models) != 1 {
					t.Fatalf("picker rows = %+v", ev.Models)
				}
				return
			}
		case <-timeout:
			t.Fatal("no picker event")
		}
	}
}
