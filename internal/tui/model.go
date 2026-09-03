package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"zeuf/internal/agent"
)

// Modes of the root model. Overlays (picker/wizard/approval/help) capture
// input until dismissed; the agent core is untouched by any of them.
type mode int

const (
	modeChat mode = iota
	modePicker
	modeConnect
	modeApproval
	modeHelp
)

// block is one rendered conversation unit.
type block struct {
	kind string // "user","assistant","tool","system","notice","error"
	text string
}

// Status drives the status bar.
type Status struct {
	Model    string
	Provider string
	State    string
	Plan     string
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	noticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	toolStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	userStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("99")).Padding(1, 2)
)

// Model is the Zeuf terminal interface.
type Model struct {
	mode        mode
	prevMode    mode // where approval/help returns to
	blocks      []block
	input       textarea.Model
	vp          viewport.Model
	status      Status
	fallbacks   int
	width       int
	height      int
	history     []string
	histIdx     int
	busy        bool
	quitting    bool
	events      chan Event
	submit      chan<- string
	actions     chan<- Action
	pendingAppr *agent.ApprovalReq

	picker  *pickerState
	connect *connectState
}

// New builds the TUI model reading core events.
func New(events chan Event) Model {
	return NewFull(events, nil, nil)
}

// NewWithSubmit forwards entered lines to the agent core.
func NewWithSubmit(events chan Event, submit chan<- string) Model {
	return NewFull(events, submit, nil)
}

// NewFull wires events, line submission and UI actions.
func NewFull(events chan Event, submit chan<- string, actions chan<- Action) Model {
	ta := textarea.New()
	ta.Placeholder = "Ask Zeuf…  (/models · /connect · ? for help)"
	ta.ShowLineNumbers = false
	ta.SetHeight(3)
	ta.Focus()
	return Model{
		events:  events,
		submit:  submit,
		actions: actions,
		input:   ta,
		status:  Status{State: "Starting"},
		histIdx: -1,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return waitEvent(m.events) }

func waitEvent(ch chan Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return quitMsg{}
		}
		return ev
	}
}

type quitMsg struct{}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(10, msg.Width-6))
		headerH, footerH := 2, 6
		m.vp = viewport.New(max(10, msg.Width-2), max(5, msg.Height-headerH-footerH))
		m.vp.SetContent(m.renderBlocks(m.vp.Width))
		if m.picker != nil {
			m.picker.list.SetSize(max(10, msg.Width-8), max(5, msg.Height-10))
		}
		return m, nil
	case quitMsg:
		m.quitting = true
		return m, tea.Quit
	case Event:
		m.handleEvent(msg)
		return m, waitEvent(m.events)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Route component updates by mode.
	var cmd tea.Cmd
	switch m.mode {
	case modePicker:
		if m.picker != nil {
			m.picker.list, cmd = m.picker.list.Update(msg)
		}
	case modeConnect:
		if m.connect != nil {
			cmd = m.connect.update(msg)
		}
	default:
		m.input, cmd = m.input.Update(msg)
		m.vp, _ = m.vp.Update(msg)
	}
	return m, cmd
}

func (m *Model) handleEvent(ev Event) {
	switch ev.Kind {
	case "token":
		m.busy = true
		if n := len(m.blocks); n > 0 && m.blocks[n-1].kind == "assistant" {
			m.blocks[n-1].text += ev.Text
		} else {
			m.blocks = append(m.blocks, block{kind: "assistant", text: ev.Text})
		}
	case "text":
		m.blocks = append(m.blocks, block{kind: "assistant", text: ev.Text})
	case "tool":
		m.blocks = append(m.blocks, block{kind: "tool", text: ev.Text})
	case "switch":
		m.fallbacks++
		m.blocks = append(m.blocks, block{kind: "notice", text: "Model limit reached. Continuing with " + ev.Text + "."})
	case "status":
		parts := strings.SplitN(ev.Text, "|", 3)
		if len(parts) == 3 {
			m.status.Model, m.status.Provider, m.status.State = parts[0], parts[1], parts[2]
			if m.busy && m.status.State == "Connected" {
				m.status.State = "Working…"
			}
		}
	case "plan":
		m.status.Plan = ev.Text
	case "error":
		m.busy = false
		m.blocks = append(m.blocks, block{kind: "error", text: ev.Text})
	case "done":
		m.busy = false
	case "picker":
		m.openPicker(ev.Models)
	case "connect-open":
		m.openConnect()
	case "approval":
		if ev.Approval != nil {
			m.pendingAppr = ev.Approval
			m.prevMode = m.mode
			m.mode = modeApproval
		}
	}
	m.trimBlocks()
	m.refreshViewport()
}

func (m *Model) trimBlocks() {
	if len(m.blocks) > 800 {
		m.blocks = append([]block{{kind: "system", text: "… earlier history trimmed …"}}, m.blocks[len(m.blocks)-800:]...)
	}
}

func (m *Model) refreshViewport() {
	stick := m.vp.AtBottom()
	m.vp.SetContent(m.renderBlocks(m.vp.Width))
	if stick {
		m.vp.GotoBottom()
	}
}

func (m *Model) renderBlocks(width int) string {
	var b strings.Builder
	for _, bl := range m.blocks {
		for _, line := range wrapLines(bl.text, max(20, width-2)) {
			switch bl.kind {
			case "user":
				b.WriteString(userStyle.Render(line) + "\n")
			case "tool":
				b.WriteString(toolStyle.Render("✓ "+line) + "\n")
			case "notice":
				b.WriteString(noticeStyle.Render(line) + "\n")
			case "error":
				b.WriteString(errStyle.Render("error: "+line) + "\n")
			case "system":
				b.WriteString(dimStyle.Render(line) + "\n")
			default:
				b.WriteString("  " + line + "\n")
			}
		}
	}
	return b.String()
}

// handleKey routes keys by mode.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch m.mode {
	case modeApproval:
		return m.handleApprovalKey(key)
	case modeHelp:
		m.mode = m.prevMode
		return m, nil
	case modePicker:
		return m.handlePickerKey(key, msg)
	case modeConnect:
		return m.handleConnectKey(key, msg)
	}

	// Chat mode.
	switch key {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "ctrl+p":
		if m.actions != nil {
			m.actions <- ActionOpenPicker{}
		}
		return m, nil
	case "?":
		if strings.TrimSpace(m.input.Value()) == "" {
			m.prevMode = m.mode
			m.mode = modeHelp
			return m, nil
		}
	case "pgup", "ctrl+u":
		m.vp.HalfViewUp()
		return m, nil
	case "pgdown", "ctrl+d":
		m.vp.HalfViewDown()
		return m, nil
	case "up", "down":
		if m.tryHistory(key) {
			return m, nil
		}
	case "ctrl+j":
		m.input.SetValue(m.input.Value() + "\n")
		return m, nil
	case "enter":
		line := strings.TrimSpace(m.input.Value())
		m.input.SetValue("")
		m.histIdx = -1
		if line == "" {
			return m, nil
		}
		m.history = append(m.history, line)
		m.blocks = append(m.blocks, block{kind: "user", text: "> " + line})
		m.refreshViewport()
		if line == "/quit" || line == "/exit" {
			m.quitting = true
			if m.submit != nil {
				select {
				case m.submit <- line:
				default:
				}
			}
			return m, tea.Quit
		}
		if m.submit != nil {
			m.busy = true
			select {
			case m.submit <- line:
			default:
			}
		}
		return m, nil
	case "esc":
		if m.input.Value() != "" {
			m.input.SetValue("")
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.vp, _ = m.vp.Update(msg)
	return m, cmd
}

func (m *Model) tryHistory(key string) bool {
	if len(m.history) == 0 || strings.Contains(m.input.Value(), "\n") {
		return false
	}
	if key == "up" {
		if m.histIdx < len(m.history)-1 {
			m.histIdx++
		}
	} else {
		if m.histIdx > 0 {
			m.histIdx--
		} else {
			return false
		}
	}
	m.input.SetValue(m.history[len(m.history)-1-m.histIdx])
	return true
}

func (m Model) handleApprovalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y":
		m.answerApproval(true)
	case "n", "N", "esc":
		m.answerApproval(false)
	}
	return m, nil
}

func (m *Model) answerApproval(ok bool) {
	if m.pendingAppr != nil {
		select {
		case m.pendingAppr.Resp <- ok:
		default:
		}
		m.blocks = append(m.blocks, block{kind: "system", text: approvalVerdict(ok, m.pendingAppr.Action)})
		m.pendingAppr = nil
	}
	m.mode = m.prevMode
	if m.mode != modeChat {
		m.mode = modeChat
	}
	m.refreshViewport()
}

func approvalVerdict(ok bool, action string) string {
	if ok {
		return "Approved: " + action
	}
	return "Denied: " + action + " (told the agent to work around it)"
}

// View implements tea.Model.
func (m Model) View() string {
	if m.width == 0 {
		return "starting…"
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("✦ Zeuf") + dimStyle.Render("  your coding agent · ? help · /models · /connect · /quit") + "\n")
	switch m.mode {
	case modePicker:
		b.WriteString(m.pickerView())
	case modeConnect:
		b.WriteString(m.connectView())
	default:
		b.WriteString(m.vp.View() + "\n")
	}
	b.WriteString(m.statusView() + "\n")
	if m.mode == modeApproval && m.pendingAppr != nil {
		b.WriteString(m.approvalView())
	} else if m.mode == modeHelp {
		b.WriteString(m.helpView())
	} else if m.mode == modeChat {
		b.WriteString(boxStyle.Render(m.input.View()))
	}
	if m.quitting {
		return b.String() + "\n"
	}
	return b.String()
}

func (m Model) statusView() string {
	dot := "●"
	if m.status.State != "Connected" && m.status.State != "Working…" {
		dot = "○"
	}
	plan := ""
	if m.status.Plan != "" {
		plan = fmt.Sprintf("   Plan: %s", m.status.Plan)
	}
	state := m.status.State
	if m.busy && state == "Connected" {
		state = "Working…"
	}
	return dimStyle.Render(fmt.Sprintf("  %s · %s · fallbacks %d · %s %s%s",
		nonEmpty(m.status.Model, "—"), nonEmpty(m.status.Provider, "—"),
		m.fallbacks, dot, state, plan))
}

func (m Model) approvalView() string {
	p := m.pendingAppr
	body := fmt.Sprintf("Zeuf wants approval\n\n%s\n%s\n\n[y] approve   [n] deny",
		p.Action, dimStyle.Render(truncRunes(p.Detail, 300)))
	return boxStyle.Render(noticeStyle.Render(body))
}

func (m Model) helpView() string {
	return boxStyle.Render(`Keys: enter send · ctrl+j newline · ↑/↓ history · pgup/pgdn scroll · ctrl+p models · ? help · ctrl+c quit

Commands: /models [all] — pick the active model · /connect — attach a backend
  /router auto|balanced|fastest|quality|pin <id>|unpin|fallback|nofallback
  /providers · /session · /quit

any key closes this help`)
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// wrapLines greedy-wraps plain text to width (call before styling).
func wrapLines(s string, width int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		words := strings.Fields(para)
		line := ""
		for _, w := range words {
			if line == "" {
				line = w
				continue
			}
			if len([]rune(line))+1+len([]rune(w)) > width {
				out = append(out, line)
				line = w
			} else {
				line += " " + w
			}
		}
		out = append(out, line)
	}
	return out
}
