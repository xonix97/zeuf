package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"zeuf/internal/config"
)

// connectState is the /connect wizard: kind → fields → credential → done.
type connectState struct {
	step     int // 0 kind, 1 fields, 2 credential kind, 3 credential value, 4 confirm/login-note
	cursor   int
	preset   config.Preset
	name     string
	baseURL  string
	isLogin  bool // CLI login path (opencode/kilo/gemini)
	loginBin string
	loginCmd string // how the user logs in, e.g. "opencode auth login"
	inputs   []textinput.Model
	focus    int
	credKind int // 0 env, 1 paste
	note     string
}

func newConnectState() *connectState {
	return &connectState{}
}

func (m *Model) openConnect() {
	m.connect = newConnectState()
	m.mode = modeConnect
}

func connectKinds() []string {
	kinds := make([]string, 0, len(config.Presets)+3)
	for _, p := range config.Presets {
		kinds = append(kinds, p.Title)
	}
	return append(kinds,
		"OpenCode login (in your terminal)",
		"Kilo login (in your terminal)",
		"Gemini login (in your terminal)",
	)
}

// loginCommand tells the user how to authenticate each CLI backend.
func loginCommand(bin string) string {
	if bin == "gemini" {
		return "gemini"
	}
	return bin + " auth login"
}

func (m Model) connectView() string {
	c := m.connect
	if c == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Connect a model backend") + dimStyle.Render("   esc cancels") + "\n\n")
	switch c.step {
	case 0:
		for i, k := range connectKinds() {
			cursor := "  "
			if i == c.cursor {
				cursor = "▸ "
			}
			b.WriteString(cursor + k + "\n")
		}
		b.WriteString(dimStyle.Render("\n↑/↓ + enter (or 1-8)"))
	case 1:
		b.WriteString("Endpoint details  (tab/enter moves, esc back)\n\n")
		for i, inp := range c.inputs {
			label := []string{"Name:      ", "Base URL:  "}[i]
			b.WriteString(label + inp.View() + "\n")
		}
	case 2:
		opts := []string{"Use an environment variable", "Paste the key now (stored securely)"}
		b.WriteString("Credential\n\n")
		for i, o := range opts {
			cursor := "  "
			if i == c.credKind {
				cursor = "▸ "
			}
			b.WriteString(cursor + o + "\n")
		}
		b.WriteString(dimStyle.Render("\n↑/↓ + enter"))
	case 3:
		if c.credKind == 0 {
			b.WriteString("Environment variable name:\n\n" + c.inputs[0].View())
		} else {
			b.WriteString("Paste API key (hidden):\n\n" + c.inputs[0].View())
		}
		b.WriteString(dimStyle.Render("\n\nenter saves · esc back"))
	case 4:
		b.WriteString(c.note + "\n")
		b.WriteString(dimStyle.Render("\nenter closes"))
	}
	return boxStyle.Render(b.String())
}

func (m Model) handleConnectKey(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := m.connect
	if c == nil {
		m.mode = modeChat
		return m, nil
	}
	if key == "esc" {
		if c.step == 0 {
			m.connect = nil
			m.mode = modeChat
			return m, nil
		}
		c.step--
		if c.step == 1 {
			c.focus = 0
			c.focusInputs()
		}
		return m, nil
	}
	switch c.step {
	case 0:
		n := len(connectKinds())
		switch key {
		case "up":
			c.cursor = (c.cursor + n - 1) % n
		case "down":
			c.cursor = (c.cursor + 1) % n
		case "enter":
			c.chooseKind(c.cursor)
		default:
			if len(key) == 1 && key[0] >= '1' && int(key[0]-'1') < n {
				c.chooseKind(int(key[0] - '1'))
			}
		}
		return m, nil
	case 1:
		switch key {
		case "tab", "shift+tab", "up", "down":
			c.focus = (c.focus + 1) % len(c.inputs)
			if key == "shift+tab" || key == "up" {
				c.focus = (c.focus + len(c.inputs) - 1) % len(c.inputs)
			}
			c.focusInputs()
			return m, nil
		case "enter":
			if c.focus < len(c.inputs)-1 {
				c.focus++
				c.focusInputs()
				return m, nil
			}
			c.name = strings.TrimSpace(c.inputs[0].Value())
			c.baseURL = strings.TrimSpace(c.inputs[1].Value())
			if c.name == "" {
				c.focus = 0
				c.focusInputs()
				return m, nil
			}
			if !validURL(c.baseURL) {
				c.focus = 1
				c.focusInputs()
				return m, nil
			}
			if c.preset.KeyEnv == "" && c.preset.ID != "custom" {
				m.finishConnect("", "")
				return m, nil
			}
			c.step = 2
			return m, nil
		}
		c.inputs[c.focus], _ = c.inputs[c.focus].Update(msg)
		return m, nil
	case 2:
		switch key {
		case "up", "down", "tab":
			c.credKind = 1 - c.credKind
		case "enter":
			c.inputs = []textinput.Model{newInput("", c.credKind == 1)}
			if c.credKind == 0 && c.preset.KeyEnv != "" {
				c.inputs[0].SetValue(c.preset.KeyEnv)
			}
			c.inputs[0].Focus()
			c.step = 3
		}
		return m, nil
	case 3:
		if key == "enter" {
			v := strings.TrimSpace(c.inputs[0].Value())
			if v == "" {
				return m, nil
			}
			if c.credKind == 0 {
				m.finishConnect(v, "")
			} else {
				m.finishConnect("", v)
				c.inputs[0].SetValue("")
			}
			return m, nil
		}
		c.inputs[0], _ = c.inputs[0].Update(msg)
		return m, nil
	case 4:
		if key == "enter" {
			m.connect = nil
			m.mode = modeChat
		}
		return m, nil
	}
	return m, nil
}

func (c *connectState) chooseKind(idx int) {
	nk := len(config.Presets)
	if idx < nk {
		c.preset = config.Presets[idx]
		c.isLogin = false
		c.inputs = []textinput.Model{newInput(c.preset.ID, false), newInput(c.preset.BaseURL, false)}
		c.focus = 0
		c.focusInputs()
		c.step = 1
		return
	}
	c.isLogin = true
	if idx == nk {
		c.loginBin = "opencode"
	} else if idx == nk+1 {
		c.loginBin = "kilo"
	} else {
		c.loginBin = "gemini"
	}
	c.loginCmd = loginCommand(c.loginBin)
	c.step = 4
	c.note = ""
}

func (c *connectState) focusInputs() {
	for i := range c.inputs {
		if i == c.focus {
			c.inputs[i].Focus()
		} else {
			c.inputs[i].Blur()
		}
	}
}

// update handles bubbletea Msgs for text inputs (kept for completeness;
// keys are routed via handleConnectKey).
func (c *connectState) update(msg tea.Msg) tea.Cmd { return nil }

// finishConnect emits the core action (or login rescan) and shows the note.
func (m *Model) finishConnect(keyEnv, secret string) {
	c := m.connect
	if c.isLogin {
		if m.actions != nil {
			m.actions <- ActionLogin{Backend: c.loginBin}
		}
		c.note = fmt.Sprintf("Run `%s` in your normal terminal.\nZeuf will rescan models when you return.", c.loginCmd)
		c.step = 4
		m.blocks = append(m.blocks, block{kind: "system", text: "Login requested for " + c.loginBin + "; rescanning on return."})
		m.refreshViewport()
		return
	}
	if m.actions != nil {
		m.actions <- ActionConnect{
			Name: c.name, BaseURL: c.baseURL,
			KeyEnv: keyEnv, Secret: secret,
		}
	}
	c.note = "Saving… Zeuf will announce the new backend once connected."
	c.step = 4
}

func newInput(value string, password bool) textinput.Model {
	ti := textinput.New()
	ti.SetValue(value)
	if password {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}
	ti.Width = 48
	return ti
}

func validURL(u string) bool {
	u = strings.TrimSpace(u)
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}
