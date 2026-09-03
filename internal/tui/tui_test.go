package tui

import (
	"strings"
	"testing"

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

func TestApprovalModal(t *testing.T) {
	events := make(chan Event, 8)
	m := NewFull(events, nil, nil)
	m.width, m.height = 80, 24
	resp := make(chan bool, 1)
	m.handleEvent(Event{Kind: "approval", Approval: &agent.ApprovalReq{ID: "t", Action: "write file", Detail: "/x", Resp: resp}})
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
