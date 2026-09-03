package cli

import (
	"testing"

	"zeuf/internal/agent"
	ct "zeuf/internal/core/tools"
	"zeuf/internal/mcp"
	"zeuf/internal/router"
)

func slashHarness(t *testing.T) (*router.Prefs, *router.Registry, *agent.Session2, *agent.Agent, *ct.Registry, *mcp.Manager) {
	t.Helper()
	tools := ct.NewRegistry(t.TempDir(), ct.Policy{AutoApprove: true})
	reg := router.NewRegistry()
	prefs := router.DefaultPrefs()
	sess := agent.NewSession("slash-test", "", tools)
	ag := agent.New(router.New(reg), tools)
	return &prefs, reg, sess, ag, tools, mcp.NewManager()
}

func TestSlashStateCommands(t *testing.T) {
	prefs, reg, sess, ag, tools, mgr := slashHarness(t)
	// None of these may block on stdin or fail without sessions.
	for _, line := range []string{"/sessions", "/resume nope", "/rewind", "/checkpoints", "/skills", "/skill nope", "/mcp", "/agents", "/providers", "/session", "/bogus"} {
		if quit := handleSlash(line, prefs, reg, sess, ag, tools, mgr); quit {
			t.Errorf("%s should not quit", line)
		}
	}
	if quit := handleSlash("/quit", prefs, reg, sess, ag, tools, mgr); !quit {
		t.Error("/quit must quit")
	}
}
