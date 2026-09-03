package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"zeuf/internal/agent"
)

func TestWrapLines(t *testing.T) {
	got := wrapLines("aa bb cc dd", 5)
	if len(got) != 2 || got[0] != "aa bb" || got[1] != "cc dd" {
		t.Errorf("wrap = %q", got)
	}
	if got := wrapLines("a\n\nb", 80); len(got) != 3 || got[1] != "" {
		t.Errorf("blank lines must survive: %q", got)
	}
}

func TestPickerPinAction(t *testing.T) {
	events := make(chan Event, 8)
	actions := make(chan Action, 8)
	m := NewFull(events, nil, actions)
	m.width, m.height = 80, 24
	m.openPicker([]PickerModel{{FullID: "kilo/x", Display: "X", Detail: "free"}})
	if m.mode != modePicker {
		t.Fatal("picker did not open")
	}
	press := func(k tea.KeyType) {
		updated, _ := m.Update(tea.KeyMsg{Type: k})
		m = updated.(Model)
	}
	press(tea.KeyDown) // move past Auto onto the model row
	press(tea.KeyEnter)
	if m.mode != modeChat {
		t.Error("enter should close picker")
	}
	select {
	case a := <-actions:
		pin, ok := a.(ActionPin)
		if !ok || pin.FullID != "kilo/x" {
			t.Errorf("action = %#v", a)
		}
	default:
		t.Error("no pin action emitted")
	}
}

func TestPickerFuzzyLive(t *testing.T) {
	events := make(chan Event, 8)
	m := NewFull(events, nil, nil)
	m.width, m.height = 80, 24
	m.openPicker([]PickerModel{
		{FullID: "kilo/deepseek-chat", Display: "DeepSeek Chat", Detail: "kilo free"},
		{FullID: "opencode/big-pickle", Display: "Big Pickle", Detail: "opencode free"},
	})
	// Fast cursor blink: the resolver would otherwise wait out real
	// half-second blink timers on every keystroke.
	m.picker.list.FilterInput.Cursor.BlinkSpeed = time.Millisecond
	// Search bar must be live immediately — no leading "/" needed.
	if m.picker.list.FilterInput.Value() != "" {
		t.Fatal("filter should start empty")
	}
	for _, r := range []rune("dpsek") {
		m = stepPicker(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	vis := m.picker.list.VisibleItems()
	if len(vis) != 1 {
		t.Fatalf("fuzzy 'dpsek' matched %d items, want 1", len(vis))
	}
	if it, ok := vis[0].(pickerItem); !ok || it.fullID != "kilo/deepseek-chat" {
		t.Errorf("wrong match: %+v", vis[0])
	}
}

// stepPicker feeds a message and resolves resulting commands, applying
// filter matches back (bubbles filters asynchronously; blink/spinner
// ticks are ignored, with a depth bound against rescheduling loops).
func stepPicker(m Model, msg tea.Msg) Model {
	updated, cmd := m.Update(msg)
	m = updated.(Model)
	var resolve func(c tea.Cmd, depth int)
	resolve = func(c tea.Cmd, depth int) {
		if c == nil || depth > 4 {
			return
		}
		r := c()
		if r == nil {
			return
		}
		if b, ok := r.(tea.BatchMsg); ok {
			for _, cc := range b {
				if cc != nil {
					resolve(cc, depth+1)
				}
			}
			return
		}
		if cc, ok := r.(tea.Cmd); ok {
			resolve(cc, depth+1)
			return
		}
		applyFilterMsg(&m, r)
	}
	resolve(cmd, 0)
	return m
}

func applyFilterMsg(m *Model, r tea.Msg) {
	if fm, ok := r.(list.FilterMatchesMsg); ok {
		updated, _ := m.Update(fm)
		*m = updated.(Model)
	}
}

func TestPickerEscClearsFilterFirst(t *testing.T) {
	events := make(chan Event, 8)
	m := NewFull(events, nil, nil)
	m.width, m.height = 80, 24
	m.openPicker([]PickerModel{{FullID: "kilo/a", Display: "A", Detail: "x"}})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	m = updated.(Model)
	if m.picker.list.FilterInput.Value() == "" {
		t.Fatal("typing should fill the search bar")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(Model)
	if m.mode != modePicker {
		t.Fatal("first esc should clear the filter, not close")
	}
	if m.picker.list.FilterInput.Value() != "" {
		t.Error("filter should be cleared")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(Model)
	if m.mode != modeChat {
		t.Error("second esc should close the picker")
	}
}

func TestConnectWizardFlow(t *testing.T) {
	events := make(chan Event, 8)
	actions := make(chan Action, 8)
	m := NewFull(events, nil, actions)
	m.width, m.height = 100, 30
	m.openConnect()
	if m.mode != modeConnect {
		t.Fatal("wizard did not open")
	}
	key := func(s string) {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
		m = updated.(Model)
	}
	pressEnter := func() {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(Model)
	}
	// Choose preset 1 (OpenRouter), keep defaults, env credential, suggested var.
	key("1")
	pressEnter() // name -> baseURL
	pressEnter() // confirm fields -> credential choice
	pressEnter() // env -> value step (prefilled suggestion)
	pressEnter() // save
	select {
	case a := <-actions:
		c, ok := a.(ActionConnect)
		if !ok {
			t.Fatalf("action = %#v", a)
		}
		if c.Name != "openrouter" || !strings.HasPrefix(c.BaseURL, "https://") || c.KeyEnv == "" {
			t.Errorf("connect action wrong: %+v", c)
		}
	default:
		t.Error("no connect action emitted")
	}
}

func approvalModel() (Model, chan bool, chan Action) {
	events := make(chan Event, 8)
	actions := make(chan Action, 8)
	m := NewFull(events, nil, actions)
	m.width, m.height = 80, 24
	resp := make(chan bool, 1)
	m.handleEvent(Event{Kind: "approval", Approval: &agent.ApprovalReq{ID: "t", Action: "write file", Detail: "/x", Resp: resp}})
	return m, resp, actions
}

func TestApprovalModal(t *testing.T) {
	m, resp, _ := approvalModel()
	if m.mode != modeApproval {
		t.Fatal("approval modal did not open")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(Model)
	if m.mode != modeChat {
		t.Error("approval should close modal")
	}
	select {
	case ok := <-resp:
		if !ok {
			t.Error("expected approval")
		}
	default:
		t.Error("UI never answered the request")
	}
}

func TestApprovalNavigateAndAlways(t *testing.T) {
	m, resp, actions := approvalModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.apprIdx != 1 {
		t.Fatalf("right must move to always, idx=%d", m.apprIdx)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = updated
	select {
	case ok := <-resp:
		if !ok {
			t.Error("expected approval")
		}
	default:
		t.Fatal("UI never answered")
	}
	select {
	case a := <-actions:
		allow, ok := a.(ActionAllowAlways)
		if !ok || allow.Tool != "write file" {
			t.Errorf("action = %#v", a)
		}
	default:
		t.Error("always must emit ActionAllowAlways")
	}
}

func TestApprovalDeny(t *testing.T) {
	m, resp, _ := approvalModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	_ = updated
	select {
	case ok := <-resp:
		if ok {
			t.Error("expected denial")
		}
	default:
		t.Error("UI never answered")
	}
	if view := stripANSI(m.approvalView()); !strings.Contains(view, "allow once") || !strings.Contains(view, "allow always") || !strings.Contains(view, "reject") {
		t.Errorf("modal must offer three options:\n%s", view)
	}
}

func TestMCPFooter(t *testing.T) {
	m := testModel()
	if bar := stripANSI(m.statusView()); strings.Contains(bar, "MCP") {
		t.Errorf("unknown MCP state must hide the segment: %q", bar)
	}
	m.handleEvent(Event{Kind: "mcp", Text: "2", Detail: "0"})
	if bar := stripANSI(m.statusView()); !strings.Contains(bar, "2 MCP") {
		t.Errorf("bar = %q", bar)
	}
	m.handleEvent(Event{Kind: "mcp", Text: "0", Detail: "1"})
	if bar := stripANSI(m.statusView()); !strings.Contains(bar, "MCP") {
		t.Errorf("failed MCP must still show: %q", bar)
	}
}
