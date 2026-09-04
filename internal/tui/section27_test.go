package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"zeuf/internal/agent"
)

func TestSection27Header(t *testing.T) {
	m := testModel()
	m.width = 120
	m.handleEvent(Event{Kind: "session", Detail: "/home/dev/myapp|feat/auth|*"})
	m.handleEvent(Event{Kind: "status", Text: "deepseek-coder|kilo|Connected|DeepSeek Coder"})
	m.handleEvent(Event{Kind: "usage", Text: "15k/128k", Detail: "12%"})
	m.handleEvent(Event{Kind: "phase", Text: "PLANNING"})
	m.handleEvent(Event{Kind: "task", Text: "Fix authentication timeout in client"})
	m.busy = true

	view := m.headerView()
	plain := stripANSI(view)

	// Check pill badge
	if !strings.Contains(plain, "ZEUF") {
		t.Errorf("header missing ZEUF badge: %s", plain)
	}
	// Check directory
	if !strings.Contains(plain, "DIR: /home/dev/myapp") {
		t.Errorf("header missing DIR: %s", plain)
	}
	// Check git branch and dirty indicator
	if !strings.Contains(plain, "GIT: ⎇ feat/auth*") {
		t.Errorf("header missing GIT: %s", plain)
	}
	// Check model
	if !strings.Contains(plain, "DeepSeek Coder") || !strings.Contains(plain, "kilo") {
		t.Errorf("header missing model info: %s", plain)
	}
	// Check context tokens
	if !strings.Contains(plain, "CTX: 15k/128k (12%)") {
		t.Errorf("header missing CTX: %s", plain)
	}
	// Check phase badge
	if !strings.Contains(plain, "PLANNING") {
		t.Errorf("header missing PHASE: %s", plain)
	}
	// Check active task row
	if !strings.Contains(plain, "🎯 Task › Fix authentication timeout in client") {
		t.Errorf("header missing active task: %s", plain)
	}
}

func TestSection27TreePlan(t *testing.T) {
	m := testModel()
	m.width = 100
	m.busy = true

	g := agent.NewTaskGraph("Fix auth timeout")
	g.AddTask(&agent.Task{
		ID:            "T1",
		Title:         "Inspect auth flow",
		AssignedAgent: "explorer",
		Status:        agent.TaskCompleted,
	})
	g.AddTask(&agent.Task{
		ID:            "T2",
		Title:         "Reproduce timeout",
		AssignedAgent: "tester",
		Dependencies:  []string{"T1"},
		Status:        agent.TaskCompleted,
	})
	g.AddTask(&agent.Task{
		ID:            "T3",
		Title:         "Implement fix",
		AssignedAgent: "implementer",
		Dependencies:  []string{"T2"},
		Status:        agent.TaskRunning,
	})
	g.AddTask(&agent.Task{
		ID:            "T4",
		Title:         "Run verification suite",
		AssignedAgent: "tester",
		Dependencies:  []string{"T3"},
		Status:        agent.TaskBlocked,
	})

	m.handleEvent(Event{
		Kind:  "plan",
		Text:  "2/4",
		Graph: g,
	})

	view := stripANSI(m.renderBlocks(100))

	// Tree connectors and statuses
	if !strings.Contains(view, "Planning (2/4 completed)") {
		t.Errorf("header missing: %s", view)
	}
	if !strings.Contains(view, "├─ ✓ [T1] Inspect auth flow (explorer)") {
		t.Errorf("T1 missing: %s", view)
	}
	if !strings.Contains(view, "├─ ✓ [T2] Reproduce timeout (tester)") {
		t.Errorf("T2 missing: %s", view)
	}
	if !strings.Contains(view, "├─ ● [T3] Implement fix (implementer)") {
		t.Errorf("T3 missing: %s", view)
	}
	if !strings.Contains(view, "└─ ⊘ [T4] Run verification suite (tester)") {
		t.Errorf("T4 missing: %s", view)
	}
	if !strings.Contains(view, "waiting for: T3") {
		t.Errorf("T4 dependency waiting missing: %s", view)
	}
}

func TestSection27AgentActivity(t *testing.T) {
	m := testModel()
	m.width = 100

	// Start explorer subagent
	m.handleEvent(Event{
		Kind:   "agent-start",
		Role:   "explorer",
		TaskID: "T1",
		Text:   "Investigating retry behavior in internal/auth...",
		Depth:  1,
	})

	viewStart := stripANSI(m.renderBlocks(100))
	if !strings.Contains(viewStart, "● Agent [T1] [explorer] (running)") {
		t.Errorf("agent-start header missing: %s", viewStart)
	}
	if !strings.Contains(viewStart, "└─ Investigating retry behavior in internal/auth...") {
		t.Errorf("agent-start detail missing: %s", viewStart)
	}

	// Complete explorer subagent
	m.handleEvent(Event{
		Kind:     "agent-end",
		Role:     "explorer",
		TaskID:   "T1",
		Text:     "Found token expiry without backoff in client.go",
		Ok:       true,
		Duration: 2100 * time.Millisecond,
		Depth:    1,
	})

	viewEnd := stripANSI(m.renderBlocks(100))
	if !strings.Contains(viewEnd, "✓ Agent [T1] [explorer] (2.1s)") {
		t.Errorf("agent-end header missing: %s", viewEnd)
	}
	if !strings.Contains(viewEnd, "└─ Found token expiry without backoff in client.go") {
		t.Errorf("agent-end detail missing: %s", viewEnd)
	}
}

func TestSection27Verification(t *testing.T) {
	m := testModel()
	m.width = 100

	// Start verification
	m.handleEvent(Event{
		Kind: "verify-start",
		Text: "go test ./internal/auth/...",
	})

	viewStart := stripANSI(m.renderBlocks(100))
	if !strings.Contains(viewStart, "VERIFY") || !strings.Contains(viewStart, "● go test ./internal/auth/... (running)") {
		t.Errorf("verify-start missing: %s", viewStart)
	}

	// Successful verification
	m.handleEvent(Event{
		Kind:     "verify-end",
		Text:     "go test ./internal/auth/...",
		Ok:       true,
		Duration: 1200 * time.Millisecond,
	})

	viewPass := stripANSI(m.renderBlocks(100))
	if !strings.Contains(viewPass, "✓ go test ./internal/auth/... (1.2s)") {
		t.Errorf("verify success missing: %s", viewPass)
	}

	// Failed verification triggering repair
	m.handleEvent(Event{
		Kind:     "verify-end",
		Text:     "go test ./...",
		Ok:       false,
		Duration: 3400 * time.Millisecond,
		Detail:   "TestAuthTimeout: client_test.go:42: deadline exceeded",
	})

	viewFail := stripANSI(m.renderBlocks(100))
	if !strings.Contains(viewFail, "✗ go test ./... (3.4s)") {
		t.Errorf("verify fail missing: %s", viewFail)
	}
	if !strings.Contains(viewFail, "⎿ TestAuthTimeout: client_test.go:42: deadline exceeded") {
		t.Errorf("verify diagnosis missing: %s", viewFail)
	}
	if !strings.Contains(viewFail, "→ creating repair task") {
		t.Errorf("repair notice missing: %s", viewFail)
	}
}

func TestSection27DiffSummary(t *testing.T) {
	m := testModel()
	m.width = 100

	diffStat := ` internal/auth/client.go      | 24 +++++++++++++++++-------
 internal/auth/client_test.go | 27 +++++++++++++++++++++++++++
 2 files changed, 45 insertions(+), 6 deletions(-)`

	m.handleEvent(Event{
		Kind: "diff",
		Text: diffStat,
	})

	view := stripANSI(m.renderBlocks(100))
	if !strings.Contains(view, "CHANGES") {
		t.Errorf("CHANGES title missing: %s", view)
	}
	if !strings.Contains(view, "internal/auth/client.go") || !strings.Contains(view, "internal/auth/client_test.go") {
		t.Errorf("changed files missing: %s", view)
	}
	if !strings.Contains(view, "2 files changed, 45 insertions(+), 6 deletions(-)") {
		t.Errorf("diffstat summary missing: %s", view)
	}
}

func TestSection27ModelSwitch(t *testing.T) {
	m := testModel()
	m.width = 100

	m.handleEvent(Event{
		Kind:   "switch",
		Text:   "kilo/deepseek-coder",
		Detail: "opencode/mimo-v2|rate limit exceeded",
	})

	view := stripANSI(m.renderBlocks(100))
	if !strings.Contains(view, "MODEL SWITCH") {
		t.Errorf("MODEL SWITCH header missing: %s", view)
	}
	if !strings.Contains(view, "opencode/mimo-v2") {
		t.Errorf("from model missing: %s", view)
	}
	if !strings.Contains(view, "kilo/deepseek-coder") {
		t.Errorf("to model missing: %s", view)
	}
	if !strings.Contains(view, "reason: rate limit exceeded") {
		t.Errorf("reason missing: %s", view)
	}
	if !strings.Contains(view, "session preserved") {
		t.Errorf("session preserved missing: %s", view)
	}
}

func TestSection27ThreeColumnLayout(t *testing.T) {
	m := testModel()
	m.width = 120
	m.height = 36

	// Populate session and status
	m.handleEvent(Event{Kind: "session", Detail: "/home/dev/visudio|feat/audio-dsp|*"})
	m.handleEvent(Event{Kind: "status", Text: "claude-sonnet|anthropic|Connected|Claude Sonnet"})
	m.handleEvent(Event{Kind: "usage", Text: "18.4k / 64k", Detail: "42%"})
	m.handleEvent(Event{Kind: "task", Text: "Fix audio beat detection in Visudio"})
	m.busy = true

	// Simulate tools touching files
	m.handleEvent(Event{Kind: "tool-start", Tool: "read", Args: `{"path":"src/audio_analysis.rs"}`})
	m.handleEvent(Event{Kind: "tool-end", Tool: "read", Text: "ok", Ok: true})
	m.handleEvent(Event{Kind: "tool-start", Tool: "edit", Args: `{"path":"src/main.rs"}`})
	m.handleEvent(Event{Kind: "tool-end", Tool: "edit", Text: "ok", Ok: true})

	// Simulate agent activity
	m.handleEvent(Event{Kind: "agent-start", Role: "coder", Text: "fixing DSP filters"})

	// Simulate test passing
	m.handleEvent(Event{Kind: "verify-end", Text: "cargo test", Ok: true, Detail: "24 passed"})

	view := m.View()
	plain := stripANSI(view)

	// Left column checks
	if !strings.Contains(plain, "AGENTS") {
		t.Errorf("view missing AGENTS box: %s", plain)
	}
	if !strings.Contains(plain, "main") || !strings.Contains(plain, "coder") || !strings.Contains(plain, "researcher") || !strings.Contains(plain, "tester") {
		t.Errorf("view missing agent names: %s", plain)
	}
	if !strings.Contains(plain, "SESSIONS") {
		t.Errorf("view missing SESSIONS box: %s", plain)
	}

	// Center column checks
	if !strings.Contains(plain, "TASK") {
		t.Errorf("view missing TASK box: %s", plain)
	}
	if !strings.Contains(plain, "Fix audio beat detection in Visudio") {
		t.Errorf("view missing task title in TASK box: %s", plain)
	}

	// Right column checks
	if !strings.Contains(plain, "CONTEXT") {
		t.Errorf("view missing CONTEXT box: %s", plain)
	}
	if !strings.Contains(plain, "Claude Sonnet") {
		t.Errorf("view missing model in CONTEXT: %s", plain)
	}
	if !strings.Contains(plain, "18.4k / 64k") {
		t.Errorf("view missing context tokens in CONTEXT: %s", plain)
	}
	if !strings.Contains(plain, "2 modified") {
		t.Errorf("view missing modified files count in CONTEXT: %s", plain)
	}
	if !strings.Contains(plain, "1 passed") {
		t.Errorf("view missing tests passed in CONTEXT: %s", plain)
	}
	if !strings.Contains(plain, "audio_analysis.rs") || !strings.Contains(plain, "main.rs") {
		t.Errorf("view missing touched files in tree: %s", plain)
	}
	if !strings.Contains(plain, "✦") {
		t.Errorf("view missing decorative sparkle ✦ in CONTEXT box: %s", plain)
	}

	// Active agent indicator check
	if !m.activeAgents["coder"] {
		t.Errorf("coder should be active in activeAgents map")
	}
}

func TestSlashCommandPopup(t *testing.T) {
	m := testModel()
	m.width = 100
	m.height = 30

	// 1. Initial state: popup is hidden
	if m.showCmdPopup() {
		t.Error("popup should be hidden initially")
	}

	// 2. Typing "/" opens popup
	m.input.SetValue("/")
	if !m.showCmdPopup() {
		t.Fatal("typing / should open popup")
	}

	allCmds := m.filteredSlashCmds()
	if len(allCmds) < 10 {
		t.Errorf("expected all slash commands, got %d", len(allCmds))
	}

	popupView := stripANSI(m.cmdPopupView())
	if !strings.Contains(popupView, "COMMANDS") {
		t.Errorf("popup view missing COMMANDS title: %s", popupView)
	}
	if !strings.Contains(popupView, "/models") || !strings.Contains(popupView, "/connect") {
		t.Errorf("popup view missing common commands: %s", popupView)
	}

	// 3. Typing "/m" filters commands
	m.input.SetValue("/m")
	filtered := m.filteredSlashCmds()
	for _, c := range filtered {
		if !strings.Contains(c.name, "m") {
			t.Errorf("command %s should contain 'm'", c.name)
		}
	}

	// 4. Down arrow moves selection
	origIdx := m.cmdIdx
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.cmdIdx == origIdx && len(filtered) > 1 {
		t.Errorf("down arrow should change cmdIdx, got %d", m.cmdIdx)
	}

	// 5. Tab completes the selected command
	m.input.SetValue("/mod")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.input.Value() != "/models " {
		t.Errorf("tab should complete to '/models ', got %q", m.input.Value())
	}

	// 6. After tab completes with space, popup closes
	if m.showCmdPopup() {
		t.Error("popup should close once space is entered")
	}

	// 7. Esc dismisses popup
	m.input.SetValue("/")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(Model)
	if m.input.Value() != "" {
		t.Errorf("esc should clear input, got %q", m.input.Value())
	}
	if m.showCmdPopup() {
		t.Error("popup should close after esc")
	}
}

func TestSlashCommandNotStuckInBusy(t *testing.T) {
	submit := make(chan string, 4)
	m := NewFull(make(chan Event, 64), submit, nil)
	m.width, m.height = 100, 40

	// 1. Send a slash command
	m.sendLine("/models")
	if m.busy {
		t.Error("slash command must not set m.busy = true")
	}

	// 2. Next message must NOT be captured as (next)
	m.sendLine("hey")
	if m.pendingNext != "" {
		t.Errorf("subsequent message was stuck as pendingNext: %q", m.pendingNext)
	}
	for _, b := range m.blocks {
		if b.kind == "queued" {
			t.Errorf("message should not be queued as (next): %+v", b)
		}
	}
}


