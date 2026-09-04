package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
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

// block is one rendered conversation unit. Assistant blocks hold raw
// markdown while streaming and ANSI-cooked output once finalized, so the
// final text event never duplicates the stream.
type block struct {
	kind string // "user","assistant","thinking","tool","plan","system","notice","error","agent","verify","diff","switch"
	text string
	// depth marks subagent-nested content (0 = orchestrator itself).
	depth int
	// cooked holds the rendered assistant output (empty = render raw).
	cooked string
	// writeLines previews what write/edit is putting on disk.
	writeLines []string
	// tool step fields:
	toolName    string
	toolSummary string
	toolOut     string
	toolOk      bool
	toolDone    bool
	toolMs      int64
	toolStart   time.Time

	// subagent / verify / switch fields:
	agentRole     string
	agentTaskID   string
	agentDuration time.Duration
	agentOk       bool
	agentActive   bool
	fromModel     string
	switchTo      string
	switchReason  string
}

// Status drives the header and status bar.
type Status struct {
	Model    string
	Provider string
	Display  string // human model name for the selector row
	State    string
	Plan     string
	Task     string // current user request, shown while busy
	Ctx      string // "12.4k/200k" session tokens vs window
	CtxPct   string // "6%"
	MCPOk    int    // healthy MCP servers (-1 = unknown)
	MCPBad   int    // failed MCP servers
	Workdir  string
	Branch   string
	Dirty    string

	// IDE metrics:
	FilesModified int
	TestsPassed   int
	GitDiff       string
	TouchedFiles  []string
	Sessions      []string
	ActiveSession string
}

// Rotating input examples and footer tips, opencode-style.
var inputExamples = []string{
	`"Fix the failing auth test"`,
	`"Explain how sessions work here"`,
	`"Add retries to the fetch loop"`,
	`"What is the dir"`,
	`"Refactor this into a helper"`,
}

var footerTips = []string{
	"Run /connect to add a provider",
	"ctrl+p switches models mid-task",
	"Plans update live as Zeuf works",
	"Answer n — Zeuf routes around a denial",
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	noticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	toolStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	thinkStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	userStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#FF8C00")).Padding(0, 1)

	// Elite terminal IDE styles:
	orangeColor      = lipgloss.Color("#FF8C00")
	borderDim        = lipgloss.Color("#333333")
	textBright       = lipgloss.Color("#f1f2f6")
	brandBadgeStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(orangeColor).Padding(0, 1)
	gitDirtyStyle    = lipgloss.NewStyle().Bold(true).Foreground(orangeColor)
	diffAddStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	diffDelStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	diffFileStyle    = lipgloss.NewStyle().Bold(true).Foreground(textBright)
	cardBoxStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(borderDim).Padding(0, 1)
	alertBoxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(orangeColor).Padding(1, 2)
	activeInputStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(orangeColor).Padding(0, 1)
)

func roleBadgeStyle(role string) lipgloss.Style {
	switch strings.ToLower(role) {
	case "explorer":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	case "implementer":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("114"))
	case "tester":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("141"))
	case "reviewer":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("80"))
	case "researcher":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("147"))
	default:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250"))
	}
}

func phaseBadgeStyle(phase string) lipgloss.Style {
	p := strings.ToUpper(strings.TrimSpace(phase))
	switch p {
	case "DISCOVERY":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#2980b9")).Padding(0, 1)
	case "PLANNING":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#8e44ad")).Padding(0, 1)
	case "SCHEDULING":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#9b59b6")).Padding(0, 1)
	case "EXECUTING", "EXECUTION":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#16a085")).Padding(0, 1)
	case "VERIFYING", "VERIFICATION":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#d35400")).Padding(0, 1)
	case "REPLAN":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#e67e22")).Padding(0, 1)
	case "COMPLETION", "COMPLETED":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#27ae60")).Padding(0, 1)
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("246")).Background(lipgloss.Color("237")).Padding(0, 1)
	}
}

// Model is the Zeuf terminal interface.
type Model struct {
	mode        mode
	prevMode    mode // where approval/help returns to
	blocks      []block
	input       textarea.Model
	vp          viewport.Model
	spin        spinner.Model
	status      Status
	fallbacks   int
	width       int
	height      int
	history     []string
	histIdx     int
	busy        bool
	streamIdx   int  // block receiving the stream (-1 = none)
	follow      bool // viewport tracks new output (off while reading history)
	turnStart   time.Time
	turnElapsed string
	exampleIdx  int
	tipIdx      int
	pendingNext string // staged follow-up while busy (single slot)
	quitting    bool
	events      chan Event
	submit      chan<- string
	actions     chan<- Action
	pendingAppr *agent.ApprovalReq
	apprIdx     int // selected approval option

	picker  *pickerState
	connect *connectState

	graph *agent.TaskGraph

	activeAgents map[string]bool
	touchedFiles []string
	testsPassed  int
	activeTab    int
	cmdIdx       int
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
	ta.Placeholder = "Ask Zeuf..."
	ta.Prompt = "› "
	ta.ShowLineNumbers = false
	ta.SetHeight(2)
	ta.Focus()
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(orangeColor)
	return Model{
		events:       events,
		submit:       submit,
		actions:      actions,
		input:        ta,
		spin:         sp,
		status:       Status{State: "Starting", MCPOk: -1},
		histIdx:      -1,
		streamIdx:    -1,
		follow:       true,
		activeAgents: make(map[string]bool),
		touchedFiles: make([]string, 0),
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(waitEvent(m.events), m.spin.Tick)
}

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
		headerH, inputH, footerH := 2, 3, 2
		bodyH := max(8, msg.Height-headerH-inputH-footerH)
		centerW := msg.Width - 2
		if msg.Width >= 100 {
			leftW := max(20, min(24, msg.Width/5))
			rightW := max(24, min(28, msg.Width/4))
			centerW = max(30, msg.Width-leftW-rightW)
		}
		m.vp = viewport.New(max(10, centerW-4), max(5, bodyH-4))
		m.vp.SetContent(m.renderBlocks(m.vp.Width))
		if m.picker != nil {
			m.picker.list.SetSize(max(10, msg.Width-8), max(5, msg.Height-10))
		}
		return m, nil
	case quitMsg:
		m.quitting = true
		return m, tea.Quit
	case spinner.TickMsg:
		if m.busy {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			m.refreshViewport()
			return m, cmd
		}
		return m, nil
	case Event:
		m.handleEvent(msg)
		return m, waitEvent(m.events)
	case tea.MouseMsg:
		return m.handleMouse(msg)
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
		if m.showCmdPopup() {
			matches := m.filteredSlashCmds()
			if m.cmdIdx >= len(matches) {
				m.cmdIdx = 0
			}
		}
	}
	return m, cmd
}

func (m *Model) handleEvent(ev Event) {
	switch ev.Kind {
	case "token":
		m.busy = true
		if m.streamIdx < 0 || m.streamIdx >= len(m.blocks) || m.blocks[m.streamIdx].kind != "assistant" {
			m.blocks = append(m.blocks, block{kind: "assistant", depth: ev.Depth})
			m.streamIdx = len(m.blocks) - 1
		}
		m.blocks[m.streamIdx].text += ev.Text
		m.blocks[m.streamIdx].cooked = ""
	case "reasoning":
		m.busy = true
		m.appendThinking(ev.Text, ev.Depth)
	case "text":
		m.commitText(ev.Text, ev.Depth)
	case "system":
		if strings.TrimSpace(ev.Text) != "" {
			m.blocks = append(m.blocks, block{kind: "system", text: ev.Text})
		}
	case "tool-start":
		m.busy = true
		m.blocks = append(m.blocks, block{
			kind: "tool", toolName: ev.Tool,
			toolSummary: toolSummary(ev.Tool, ev.Args),
			toolStart:   time.Now(),
			depth:       ev.Depth,
			writeLines:  writePreviewLines(ev.Tool, ev.Args),
		})
		if ev.Tool == "read" || ev.Tool == "edit" || ev.Tool == "write" {
			var a map[string]any
			if err := json.Unmarshal([]byte(ev.Args), &a); err == nil {
				if p, ok := a["path"].(string); ok && p != "" {
					m.addTouchedFile(p)
				}
			}
		}
	case "tool-end":
		m.completeTool(ev.Tool, ev.Text, ev.Ok, ev.Depth)
		if ev.Tool == "edit" || ev.Tool == "write" {
			m.status.FilesModified = len(m.touchedFiles)
		}
	case "phase":
		m.status.State = ev.Text
		if ev.Detail != "" {
			m.blocks = append(m.blocks, block{kind: "notice", text: fmt.Sprintf("● %s: %s", ev.Text, ev.Detail)})
		}
	case "agent-start":
		m.busy = true
		if ev.Role != "" {
			m.activeAgents[ev.Role] = true
		}
		m.blocks = append(m.blocks, block{
			kind:        "agent",
			depth:       ev.Depth,
			agentRole:   ev.Role,
			agentTaskID: ev.TaskID,
			agentActive: true,
			text:        ev.Text,
		})
	case "agent-end":
		if ev.Role != "" {
			m.activeAgents[ev.Role] = false
		}
		m.blocks = append(m.blocks, block{
			kind:          "agent",
			depth:         ev.Depth,
			agentRole:     ev.Role,
			agentTaskID:   ev.TaskID,
			agentDuration: ev.Duration,
			agentOk:       ev.Ok,
			agentActive:   false,
			text:          ev.Text,
		})
	case "verify-start":
		m.blocks = append(m.blocks, block{
			kind:        "verify",
			text:        ev.Text,
			toolStart:   time.Now(),
			agentActive: true,
		})
	case "verify-end":
		if ev.Ok {
			m.testsPassed++
			m.status.TestsPassed++
		}
		m.blocks = append(m.blocks, block{
			kind:          "verify",
			text:          ev.Text,
			agentOk:       ev.Ok,
			agentActive:   false,
			agentDuration: ev.Duration,
			toolOut:       ev.Detail,
		})
	case "diff":
		m.status.GitDiff = parseDiffStatSummary(ev.Text)
		for _, ln := range strings.Split(ev.Text, "\n") {
			if strings.Contains(ln, "|") {
				parts := strings.SplitN(ln, "|", 2)
				f := strings.TrimSpace(parts[0])
				if f != "" && !strings.Contains(f, "changed") {
					m.addTouchedFile(f)
				}
			}
		}
		m.blocks = append(m.blocks, block{
			kind: "diff",
			text: ev.Text,
		})
	case "switch":
		m.fallbacks++
		from, reason := "", ""
		parts := strings.SplitN(ev.Detail, "|", 2)
		if len(parts) >= 1 {
			from = parts[0]
		}
		if len(parts) >= 2 {
			reason = parts[1]
		}
		m.blocks = append(m.blocks, block{
			kind:         "switch",
			fromModel:    from,
			switchTo:     ev.Text,
			switchReason: reason,
			text:         "Model switch: " + ev.Text,
		})
	case "status":
		parts := strings.SplitN(ev.Text, "|", 4)
		if len(parts) >= 3 {
			m.status.Model, m.status.Provider, m.status.State = parts[0], parts[1], parts[2]
			if len(parts) == 4 {
				m.status.Display = parts[3]
			}
		}
	case "session":
		parts := strings.SplitN(ev.Detail, "|", 3)
		if len(parts) == 3 {
			m.status.Workdir, m.status.Branch, m.status.Dirty = parts[0], parts[1], parts[2]
		}
	case "plan":
		m.status.Plan = ev.Text
		if ev.Graph != nil {
			m.graph = ev.Graph
		} else if ev.Detail == "" && ev.Text == "" {
			m.graph = nil
		}
		m.upsertPlan(ev.Detail)
	case "usage":
		m.status.Ctx, m.status.CtxPct = ev.Text, ev.Detail
	case "mcp":
		var okN, badN int
		fmt.Sscanf(ev.Text, "%d", &okN)
		fmt.Sscanf(ev.Detail, "%d", &badN)
		m.status.MCPOk, m.status.MCPBad = okN, badN
	case "task":
		m.status.Task = ev.Text
	case "error":
		m.busy = false
		m.streamIdx = -1
		m.blocks = append(m.blocks, block{kind: "error", text: ev.Text})
		m.flushPending()
	case "done":
		m.busy = false
		m.finishStream()
		if !m.turnStart.IsZero() {
			m.turnElapsed = fmtDur(time.Since(m.turnStart))
			m.turnStart = time.Time{}
		}
		m.flushPending()
	case "picker":
		m.openPicker(ev.Models)
	case "connect-open":
		m.openConnect()
	case "approval":
		if ev.Approval != nil {
			m.pendingAppr = ev.Approval
			m.apprIdx = 0
			m.prevMode = m.mode
			m.mode = modeApproval
		}
	}
	// Safety net: an idle UI must never sit on a staged follow-up, no
	// matter which event ended the turn (or arrived instead of one).
	if !m.busy && m.pendingNext != "" {
		m.flushPending()
	}
	m.trimBlocks()
	m.refreshViewport()
}

// commitText records a complete assistant message, folding it into the
// live stream block when it merely echoes streamed content. This is what
// prevents the doubled output: tokens + final text describe one message.
func (m *Model) commitText(t string, depth int) {
	if t == "" {
		return
	}
	if m.streamIdx >= 0 && m.streamIdx < len(m.blocks) && m.blocks[m.streamIdx].kind == "assistant" {
		cur := m.blocks[m.streamIdx].text
		switch {
		case t == cur:
			m.cookBlock(m.streamIdx)
			m.streamIdx = -1
			return // exact echo of the stream
		case strings.HasPrefix(t, cur):
			m.blocks[m.streamIdx].text = t // stream was a prefix; extend
			m.cookBlock(m.streamIdx)
			m.streamIdx = -1
			return
		case strings.HasSuffix(cur, t):
			m.cookBlock(m.streamIdx)
			m.streamIdx = -1
			return // trailing echo of the stream
		}
		m.streamIdx = -1
	}
	m.blocks = append(m.blocks, block{kind: "assistant", depth: depth, text: t, cooked: cookMarkdown(t, m.vp.Width)})
}

// maxThinkRunes bounds one thinking block; models can ramble.
const maxThinkRunes = 2000

// appendThinking accumulates reasoning text into the trailing thinking
// block, starting a new one after anything else.
func (m *Model) appendThinking(delta string, depth int) {
	if delta == "" {
		return
	}
	if n := len(m.blocks); n > 0 && m.blocks[n-1].kind == "thinking" && m.blocks[n-1].depth == depth {
		cur := []rune(m.blocks[n-1].text)
		if len(cur) >= maxThinkRunes {
			return
		}
		add := []rune(delta)
		if len(cur)+len(add) > maxThinkRunes {
			add = add[:maxThinkRunes-len(cur)]
			m.blocks[n-1].text = string(cur) + string(add) + "…[truncated]"
		} else {
			m.blocks[n-1].text += delta
		}
		return
	}
	m.blocks = append(m.blocks, block{kind: "thinking", text: capThink(delta), depth: depth})
}

// capThink bounds fresh thinking text the same way extensions are.
func capThink(s string) string {
	if r := []rune(s); len(r) > maxThinkRunes {
		return string(r[:maxThinkRunes]) + "…[truncated]"
	}
	return s
}

// writePreviewLines shows what write/edit is putting on disk (bounded).
// Anything else returns nil: summaries plus result previews suffice there.
func writePreviewLines(name, argsJSON string) []string {
	if argsJSON == "" {
		return nil
	}
	var a map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return nil
	}
	str := func(k string) string {
		if v, ok := a[k].(string); ok {
			return v
		}
		return ""
	}
	clip := func(s string) string {
		s = strings.TrimRight(s, "\n")
		if len([]rune(s)) > 100 {
			s = string([]rune(s)[:100]) + "…"
		}
		return s
	}
	const maxLines = 6
	switch name {
	case "write":
		var out []string
		for _, ln := range strings.Split(str("content"), "\n") {
			out = append(out, "  "+clip(ln))
			if len(out) >= maxLines {
				break
			}
		}
		if n := len(strings.Split(str("content"), "\n")) - len(out); n > 0 {
			out = append(out, dimStyle.Render("  …"))
		}
		return out
	case "edit":
		var out []string
		for _, ln := range strings.Split(str("old_string"), "\n") {
			if len(out) >= 3 {
				break
			}
			out = append(out, "- "+clip(ln))
		}
		for _, ln := range strings.Split(str("new_string"), "\n") {
			if len(out) >= maxLines {
				break
			}
			out = append(out, "+ "+clip(ln))
		}
		return out
	default:
		return nil
	}
}

// cookBlock renders one assistant block's markdown in place.
func (m *Model) cookBlock(i int) {
	if i < 0 || i >= len(m.blocks) {
		return
	}
	b := &m.blocks[i]
	if b.kind == "assistant" && b.cooked == "" && strings.TrimSpace(b.text) != "" {
		b.cooked = cookMarkdown(b.text, m.vp.Width)
	}
}

// finishStream finalizes the live block: full markdown/code/math render.
func (m *Model) finishStream() {
	m.cookBlock(m.streamIdx)
	m.streamIdx = -1
}

// completeTool closes the newest pending step with the same tool name at
// the same nesting depth.
func (m *Model) completeTool(name, preview string, ok bool, depth int) {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		b := &m.blocks[i]
		if b.kind == "tool" && !b.toolDone && b.toolName == name && b.depth == depth {
			b.toolDone = true
			b.toolOk = ok
			b.toolOut = preview
			if !b.toolStart.IsZero() {
				b.toolMs = time.Since(b.toolStart).Milliseconds()
			}
			return
		}
	}
	m.blocks = append(m.blocks, block{kind: "tool", depth: depth, toolName: name, toolSummary: name, toolOut: preview, toolOk: ok, toolDone: true})
}

// upsertPlan keeps a single live checklist block. Empty detail removes it.
func (m *Model) upsertPlan(detail string) {
	for i := range m.blocks {
		if m.blocks[i].kind == "plan" {
			if detail == "" && m.graph == nil {
				m.blocks = append(m.blocks[:i], m.blocks[i+1:]...)
			} else {
				m.blocks[i].text = detail
			}
			return
		}
	}
	if detail != "" || m.graph != nil {
		m.blocks = append(m.blocks, block{kind: "plan", text: detail})
	}
}

func (m *Model) trimBlocks() {
	if len(m.blocks) > 800 {
		kept := append([]block{{kind: "system", text: "… earlier history trimmed …"}}, m.blocks[len(m.blocks)-800:]...)
		m.blocks = kept
		m.streamIdx = -1
	}
}

func (m *Model) refreshViewport() {
	m.vp.SetContent(m.renderBlocks(m.vp.Width))
	// SetContent preserves the offset; only re-pin when following, so
	// streaming never yanks a viewport the user scrolled up to read.
	if m.follow {
		m.vp.GotoBottom()
	}
}

// toolSummary condenses raw JSON tool arguments to one readable step line.
func toolSummary(name, argsJSON string) string {
	short := func(s string, n int) string {
		s = strings.TrimSpace(s)
		if len([]rune(s)) > n {
			s = string([]rune(s)[:n]) + "…"
		}
		return s
	}
	var a map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return name + " " + short(argsJSON, 64)
	}
	str := func(k string) string {
		if v, ok := a[k].(string); ok {
			return v
		}
		return ""
	}
	switch name {
	case "read":
		return "Read " + str("path")
	case "edit":
		return "Edit " + str("path")
	case "write":
		return "Write " + str("path")
	case "bash":
		cmd := strings.SplitN(str("command"), "\n", 2)[0]
		return "Bash " + short(cmd, 64)
	case "grep":
		s := "Search " + short(str("pattern"), 48)
		if p := str("path"); p != "" {
			s += " in " + p
		}
		return s
	case "glob":
		return "Glob " + str("pattern")
	case "git":
		if raw, ok := a["args"].([]any); ok {
			parts := make([]string, 0, len(raw))
			for _, p := range raw {
				if s, ok := p.(string); ok {
					parts = append(parts, s)
				}
			}
			return "Git " + strings.Join(parts, " ")
		}
		return "Git"
	case "delegate":
		return "Delegate " + short(str("task"), 64)
	case "plan":
		s := "Plan " + str("op")
		if t := str("title"); t != "" {
			s += " " + short(t, 48)
		}
		return s
	default:
		return name + " " + short(argsJSON, 64)
	}
}

func (m *Model) renderBlocks(width int) string {
	var b strings.Builder
	for _, bl := range m.blocks {
		s := m.renderBlock(bl, width)
		if bl.depth > 0 {
			s = indentNested(s, bl.depth)
		}
		b.WriteString(s)
	}
	return b.String()
}

// indentNested guides subagent content under its parent step.
func indentNested(s string, depth int) string {
	guide := dimStyle.Render(strings.Repeat("│ ", depth))
	var b strings.Builder
	for _, ln := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString(guide + ln + "\n")
	}
	return b.String()
}

func (m *Model) renderBlock(bl block, width int) string {
	var b strings.Builder
	switch bl.kind {
	case "tool":
		b.WriteString(m.renderTool(bl, width))
	case "plan":
		b.WriteString(m.renderPlan(bl, width))
	case "agent":
		b.WriteString(m.renderAgent(bl, width))
	case "verify":
		b.WriteString(m.renderVerify(bl, width))
	case "diff":
		b.WriteString(m.renderDiff(bl, width))
	case "switch":
		b.WriteString(m.renderSwitch(bl, width))
	case "user":
		for _, line := range wrapLines(bl.text, max(20, width-2)) {
			b.WriteString(userStyle.Render(line) + "\n")
		}
	case "notice":
		for _, line := range wrapLines(bl.text, max(20, width-2)) {
			b.WriteString(noticeStyle.Render(line) + "\n")
		}
	case "error":
		for _, line := range wrapLines(bl.text, max(20, width-2)) {
			b.WriteString(errStyle.Render("error: "+line) + "\n")
		}
	case "system":
		for _, line := range wrapLines(bl.text, max(20, width-2)) {
			b.WriteString(dimStyle.Render(line) + "\n")
		}
	case "queued":
		for _, line := range wrapLines(bl.text, max(20, width-2)) {
			b.WriteString(dimStyle.Render(line) + "\n")
		}
	case "thinking":
		lines := wrapLines(bl.text, max(20, width-4))
		for i, line := range lines {
			if i == 0 {
				b.WriteString(thinkStyle.Render("◌ "+line) + "\n")
			} else {
				b.WriteString(thinkStyle.Render("  "+line) + "\n")
			}
		}
	default: // assistant
		if bl.cooked != "" {
			b.WriteString(bl.cooked + "\n")
		} else {
			for _, line := range wrapLines(bl.text, max(20, width-2)) {
				b.WriteString("  " + line + "\n")
			}
		}
	}
	return b.String()
}

func (m *Model) renderTool(bl block, width int) string {
	var b strings.Builder
	w := max(40, width-2)
	if !bl.toolDone {
		for i, ln := range wrapLines(m.spin.View()+" "+bl.toolSummary, w) {
			if i == 0 {
				b.WriteString(accentStyle.Render(ln) + "\n")
			} else {
				b.WriteString(dimStyle.Render("  "+ln) + "\n")
			}
		}
		return b.String()
	}

	mark := okStyle.Render("↳")
	statusIcon := okStyle.Render("✓")
	if !bl.toolOk {
		mark = errStyle.Render("↳")
		statusIcon = errStyle.Render("✗")
	}

	durStr := fmtDurMs(bl.toolMs)
	durStyled := dimStyle.Render(durStr)
	summary := bl.toolSummary

	plainLeft := len([]rune("✓ ↳ " + summary))
	plainDur := len([]rune(durStr))
	spaceNeed := max(2, w-plainLeft-plainDur)

	var headerLine string
	if plainLeft+plainDur+2 <= w {
		headerLine = statusIcon + " " + mark + " " + summary + strings.Repeat(" ", spaceNeed) + durStyled
	} else {
		headerLine = statusIcon + " " + mark + " " + summary + " · " + durStyled
	}
	b.WriteString(headerLine + "\n")

	for _, ln := range bl.writeLines {
		if strings.HasPrefix(ln, "+") {
			b.WriteString("  " + diffAddStyle.Render(ln) + "\n")
		} else if strings.HasPrefix(ln, "-") {
			b.WriteString("  " + diffDelStyle.Render(ln) + "\n")
		} else {
			b.WriteString(dimStyle.Render("  "+ln) + "\n")
		}
	}
	if bl.toolOut != "" {
		for _, ln := range wrapLines("⎿ "+bl.toolOut, w) {
			b.WriteString(dimStyle.Render("  "+ln) + "\n")
		}
	}
	return b.String()
}

func (m *Model) renderPlan(bl block, width int) string {
	var b strings.Builder
	type planStep struct {
		id     string
		title  string
		agent  string
		done   bool
		active bool
		failed bool
		block  bool
		deps   []string
	}

	var steps []planStep
	done, total := 0, 0

	if m.graph != nil && len(m.graph.Tasks) > 0 {
		tasks := m.graph.TasksList()
		total = len(tasks)
		currentSet := false
		for _, t := range tasks {
			ps := planStep{
				id:     t.ID,
				title:  t.Title,
				agent:  t.AssignedAgent,
				deps:   t.Dependencies,
				done:   t.Status == agent.TaskCompleted,
				active: t.Status == agent.TaskRunning,
				failed: t.Status == agent.TaskFailed,
				block:  t.Status == agent.TaskBlocked,
			}
			if ps.done {
				done++
			} else if !currentSet && !ps.failed && !ps.block {
				ps.active = true
				currentSet = true
			}
			steps = append(steps, ps)
		}
	} else {
		lines := strings.Split(bl.text, "\n")
		current := true
		for _, ln := range lines {
			ln = strings.TrimSpace(ln)
			if ln == "" {
				continue
			}
			total++
			ps := planStep{}
			if strings.HasPrefix(ln, "1:") {
				ps.done = true
				done++
				ln = strings.TrimPrefix(ln, "1:")
			} else {
				ln = strings.TrimPrefix(ln, "0:")
				if current {
					ps.active = true
					current = false
				}
			}
			ln = strings.TrimSpace(ln)
			if strings.HasPrefix(ln, "[") && strings.Contains(ln, "]") {
				closeIdx := strings.Index(ln, "]")
				ps.id = ln[1:closeIdx]
				ln = strings.TrimSpace(ln[closeIdx+1:])
			}
			if strings.HasSuffix(ln, ")") && strings.Contains(ln, "(") {
				openIdx := strings.LastIndex(ln, "(")
				ps.agent = ln[openIdx+1 : len(ln)-1]
				ln = strings.TrimSpace(ln[:openIdx])
			}
			ps.title = ln
			steps = append(steps, ps)
		}
	}

	if done == total && total > 0 {
		b.WriteString(okStyle.Render(fmt.Sprintf("✓ Plan (%d/%d completed)", done, total)) + "\n")
	} else if m.busy {
		b.WriteString(accentStyle.Render(fmt.Sprintf("● Planning (%d/%d completed)", done, total)) + "\n")
	} else {
		b.WriteString(dimStyle.Render(fmt.Sprintf("○ Plan (%d/%d completed)", done, total)) + "\n")
	}

	for i, s := range steps {
		connector := "├─ "
		if i == len(steps)-1 {
			connector = "└─ "
		}
		treePrefix := dimStyle.Render("  " + connector)

		idPart := ""
		if s.id != "" {
			idPart = "[" + s.id + "] "
		}

		agentPart := ""
		if s.agent != "" {
			agentPart = " (" + s.agent + ")"
		}

		itemContent := idPart + s.title + agentPart

		switch {
		case s.done:
			b.WriteString(treePrefix + okStyle.Render("✓ "+itemContent) + "\n")
		case s.active:
			b.WriteString(treePrefix + accentStyle.Render("● "+itemContent) + "\n")
		case s.failed:
			b.WriteString(treePrefix + errStyle.Render("✗ "+itemContent) + "\n")
		case s.block:
			b.WriteString(treePrefix + noticeStyle.Render("⊘ "+itemContent) + "\n")
		default:
			b.WriteString(treePrefix + dimStyle.Render("○ "+itemContent) + "\n")
		}

		if len(s.deps) > 0 && !s.done {
			indent := "  │    "
			if i == len(steps)-1 {
				indent = "       "
			}
			b.WriteString(indent + dimStyle.Render("waiting for: "+strings.Join(s.deps, ", ")) + "\n")
		}
	}
	return b.String()
}

func (m *Model) renderAgent(bl block, width int) string {
	var b strings.Builder
	role := bl.agentRole
	if role == "" {
		role = "subagent"
	}
	badge := roleBadgeStyle(role).Render("[" + role + "]")
	taskID := ""
	if bl.agentTaskID != "" {
		taskID = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("141")).Render("["+bl.agentTaskID+"]") + " "
	}

	if bl.agentActive {
		b.WriteString(accentStyle.Render("●") + " " + lipgloss.NewStyle().Bold(true).Render("Agent") + " " + taskID + badge + " " + dimStyle.Render("(running)") + "\n")
	} else {
		mark := okStyle.Render("✓")
		if !bl.agentOk {
			mark = errStyle.Render("✗")
		}
		dur := ""
		if bl.agentDuration > 0 {
			dur = " " + dimStyle.Render("("+fmtDur(bl.agentDuration)+")")
		}
		b.WriteString(mark + " " + lipgloss.NewStyle().Bold(true).Render("Agent") + " " + taskID + badge + dur + "\n")
	}

	if bl.text != "" {
		lines := wrapLines(bl.text, max(20, width-6))
		for i, ln := range lines {
			if i == 0 {
				b.WriteString("  " + dimStyle.Render("└─ ") + ln + "\n")
			} else {
				b.WriteString("     " + dimStyle.Render(ln) + "\n")
			}
		}
	}
	return b.String()
}

func (m *Model) renderVerify(bl block, width int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")).Render("VERIFY") + "\n")
	if bl.agentActive {
		b.WriteString("  " + accentStyle.Render("●") + " " + bl.text + " " + dimStyle.Render("(running)") + "\n")
	} else {
		mark := okStyle.Render("✓")
		if !bl.agentOk {
			mark = errStyle.Render("✗")
		}
		dur := ""
		if bl.agentDuration > 0 {
			dur = " " + dimStyle.Render("("+fmtDur(bl.agentDuration)+")")
		}
		b.WriteString("  " + mark + " " + bl.text + dur + "\n")
		if !bl.agentOk {
			if bl.toolOut != "" {
				for _, ln := range wrapLines(bl.toolOut, max(20, width-8)) {
					b.WriteString("    " + dimStyle.Render("⎿ ") + errStyle.Render(ln) + "\n")
				}
			}
			b.WriteString("    " + noticeStyle.Render("→ creating repair task") + "\n")
		}
	}
	return b.String()
}

func (m *Model) renderDiff(bl block, width int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")).Render("CHANGES") + "\n")

	lines := strings.Split(strings.TrimSpace(bl.text), "\n")
	for _, ln := range lines {
		ln = strings.TrimRight(ln, "\r")
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			continue
		}
		if strings.Contains(ln, "changed") && (strings.Contains(ln, "insertion") || strings.Contains(ln, "+")) {
			b.WriteString("  " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250")).Render(trimmed) + "\n")
			continue
		}
		if strings.Contains(ln, "|") {
			parts := strings.SplitN(ln, "|", 2)
			filePart := strings.TrimSpace(parts[0])
			statPart := strings.TrimSpace(parts[1])
			var statBuilder strings.Builder
			for _, r := range statPart {
				if r == '+' {
					statBuilder.WriteString(diffAddStyle.Render("+"))
				} else if r == '-' {
					statBuilder.WriteString(diffDelStyle.Render("-"))
				} else {
					statBuilder.WriteRune(r)
				}
			}
			b.WriteString("  " + diffFileStyle.Render(filePart) + " " + dimStyle.Render("|") + " " + statBuilder.String() + "\n")
		} else if strings.Contains(ln, "+") || strings.Contains(ln, "-") {
			parts := strings.Fields(ln)
			if len(parts) >= 2 {
				filePart := parts[0]
				rest := strings.Join(parts[1:], " ")
				b.WriteString("  " + diffFileStyle.Render(filePart) + "  " + rest + "\n")
			} else {
				b.WriteString("  " + ln + "\n")
			}
		} else {
			b.WriteString("  " + dimStyle.Render(ln) + "\n")
		}
	}
	return b.String()
}

func (m *Model) renderSwitch(bl block, width int) string {
	var inner strings.Builder
	inner.WriteString(noticeStyle.Bold(true).Render("MODEL SWITCH") + "\n\n")
	from := bl.fromModel
	if from == "" {
		from = "previous model"
	}
	to := bl.switchTo
	if to == "" {
		to = bl.text
	}
	inner.WriteString("  " + dimStyle.Render(from) + "\n")
	inner.WriteString("        " + accentStyle.Render("↓") + "\n")
	inner.WriteString("  " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).Render(to) + "\n\n")
	if bl.switchReason != "" {
		inner.WriteString("  " + dimStyle.Render("reason: "+bl.switchReason) + "\n")
	}
	inner.WriteString("  " + okStyle.Render("✓ session preserved") + "\n")
	return alertBoxStyle.Render(inner.String()) + "\n"
}

// handleMouse routes wheel scrolling to the viewport (chat mode only) so
// the wheel scrolls history instead of arriving as arrow keys. Reaching
// the bottom re-engages follow.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeChat {
		return m, nil
	}
	switch msg.Type {
	case tea.MouseWheelUp:
		m.vp.HalfViewUp()
		m.follow = false
	case tea.MouseWheelDown:
		m.vp.HalfViewDown()
		m.follow = m.vp.AtBottom()
	}
	return m, nil
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
	case "ctrl+k":
		m.prevMode = m.mode
		m.mode = modeHelp
		return m, nil
	case "tab":
		if m.showCmdPopup() {
			matches := m.filteredSlashCmds()
			if len(matches) > 0 {
				idx := m.cmdIdx % len(matches)
				sel := matches[idx]
				m.input.SetValue(sel.name + " ")
				m.input.CursorEnd()
				return m, nil
			}
		}
		m.activeTab = (m.activeTab + 1) % 3
		return m, nil
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
		m.follow = false
		return m, nil
	case "pgdown", "ctrl+d":
		m.vp.HalfViewDown()
		m.follow = m.vp.AtBottom()
		return m, nil
	case "up":
		if m.showCmdPopup() {
			matches := m.filteredSlashCmds()
			if len(matches) > 0 {
				m.cmdIdx = (m.cmdIdx - 1 + len(matches)) % len(matches)
				return m, nil
			}
		}
		if m.tryHistory(key) {
			return m, nil
		}
	case "down":
		if m.showCmdPopup() {
			matches := m.filteredSlashCmds()
			if len(matches) > 0 {
				m.cmdIdx = (m.cmdIdx + 1) % len(matches)
				return m, nil
			}
		}
		if m.tryHistory(key) {
			return m, nil
		}
	case "ctrl+j":
		m.input.SetValue(m.input.Value() + "\n")
		return m, nil
	case "enter":
		if m.showCmdPopup() {
			matches := m.filteredSlashCmds()
			if len(matches) > 0 {
				idx := m.cmdIdx % len(matches)
				sel := matches[idx]
				if sel.args != "" {
					m.input.SetValue(sel.name + " ")
					m.input.CursorEnd()
					return m, nil
				}
				line := sel.name
				m.input.SetValue("")
				m.histIdx = -1
				m.cmdIdx = 0
				m.history = append(m.history, line)
				if line == "/quit" || line == "/exit" {
					m.blocks = append(m.blocks, block{kind: "user", text: "> " + line})
					m.refreshViewport()
					m.quitting = true
					if m.submit != nil {
						select {
						case m.submit <- line:
						default:
						}
					}
					return m, tea.Quit
				}
				m.sendLine(line)
				m.refreshViewport()
				return m, nil
			}
		}
		line := strings.TrimSpace(m.input.Value())
		m.input.SetValue("")
		m.histIdx = -1
		if line == "" {
			return m, nil
		}
		m.history = append(m.history, line)
		if line == "/quit" || line == "/exit" {
			m.blocks = append(m.blocks, block{kind: "user", text: "> " + line})
			m.refreshViewport()
			m.quitting = true
			if m.submit != nil {
				select {
				case m.submit <- line:
				default:
				}
			}
			return m, tea.Quit
		}
		if m.busy {
			// A turn is running: hold exactly one follow-up, shown as
			// (next). A newer message replaces it — turns run strictly
			// one at a time, so bursts can never pile up.
			m.pendingNext = line
			m.stageQueuedBlock(line)
			m.refreshViewport()
			return m, nil
		}
		m.sendLine(line)
		m.refreshViewport()
		return m, nil
	case "esc":
		if m.showCmdPopup() {
			m.input.SetValue("")
			m.cmdIdx = 0
			return m, nil
		}
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

// sendLine forwards one user turn to the core and marks the UI busy.
func (m *Model) sendLine(line string) {
	m.blocks = append(m.blocks, block{kind: "user", text: "> " + line})
	m.turnStart = time.Now()
	m.turnElapsed = ""
	m.streamIdx = -1
	m.follow = true
	m.exampleIdx = (m.exampleIdx + 1) % len(inputExamples)
	m.tipIdx = (m.tipIdx + 1) % len(footerTips)
	m.input.Placeholder = "Ask Zeuf…  e.g. " + inputExamples[m.exampleIdx]
	if m.submit != nil {
		if !strings.HasPrefix(line, "/") {
			m.busy = true
		}
		select {
		case m.submit <- line:
		default:
		}
	}
}

// stageQueuedBlock shows (or refreshes) the single pending follow-up.
func (m *Model) stageQueuedBlock(line string) {
	text := "(next) " + line
	for i := len(m.blocks) - 1; i >= 0; i-- {
		if m.blocks[i].kind == "queued" {
			m.blocks[i].text = text
			return
		}
	}
	m.blocks = append(m.blocks, block{kind: "queued", text: text})
}

// flushPending sends the staged follow-up as a real turn. It runs when a
// turn ends; if the core can't take it yet, the slot is kept for the next
// completion instead of dropping the message.
func (m *Model) flushPending() {
	if m.pendingNext == "" {
		return
	}
	line := m.pendingNext
	if m.submit == nil {
		return
	}
	select {
	case m.submit <- line:
		m.pendingNext = ""
		m.busy = true
		m.turnStart = time.Now()
		m.turnElapsed = ""
		m.streamIdx = -1
		m.follow = true
		// Promote the queued row to a normal user row on send.
		promoted := false
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if m.blocks[i].kind == "queued" {
				m.blocks[i].kind = "user"
				m.blocks[i].text = "> " + line
				promoted = true
				break
			}
		}
		if !promoted {
			m.blocks = append(m.blocks, block{kind: "user", text: "> " + line})
		}
	default:
		// Core still saturated: keep the slot for the next completion.
	}
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
	case "left", "h":
		m.apprIdx = (m.apprIdx + len(approvalOptions) - 1) % len(approvalOptions)
	case "right", "l":
		m.apprIdx = (m.apprIdx + 1) % len(approvalOptions)
	case "y":
		m.answerApproval("once")
	case "a":
		m.answerApproval("always")
	case "n", "esc":
		m.answerApproval("reject")
	case "enter":
		m.answerApproval(approvalOptions[m.apprIdx%len(approvalOptions)])
	}
	return m, nil
}

func (m *Model) answerApproval(sel string) {
	if m.pendingAppr != nil {
		ok := sel == "once" || sel == "always"
		select {
		case m.pendingAppr.Resp <- ok:
		default:
		}
		if sel == "always" && m.actions != nil {
			select {
			case m.actions <- ActionAllowAlways{Tool: m.pendingAppr.Action}:
			default:
			}
		}
		m.blocks = append(m.blocks, block{kind: "system", text: approvalVerdict(sel, m.pendingAppr.Action)})
		m.pendingAppr = nil
	}
	m.mode = m.prevMode
	if m.mode != modeChat {
		m.mode = modeChat
	}
	m.refreshViewport()
}

func approvalVerdict(sel, action string) string {
	switch sel {
	case "always":
		return "Always allowed this session: " + action
	case "once":
		return "Approved: " + action
	default:
		return "Denied: " + action + " (told the agent to work around it)"
	}
}

// View implements tea.Model.
func (m Model) View() string {
	if m.width == 0 {
		return "starting…"
	}
	var b strings.Builder
	b.WriteString(m.headerView() + "\n")
	if m.showWelcome() && m.mode == modeChat && m.width < 100 {
		b.WriteString(m.welcomeView())
	} else {
		switch m.mode {
		case modePicker:
			b.WriteString(m.pickerView())
		case modeConnect:
			b.WriteString(m.connectView())
		default:
			if m.width >= 100 {
				b.WriteString(m.render3ColumnView() + "\n")
			} else {
				b.WriteString(m.vp.View() + "\n")
			}
		}
	}
	if m.mode == modeApproval && m.pendingAppr != nil {
		b.WriteString(m.approvalView() + "\n")
	} else if m.mode == modeHelp {
		b.WriteString(m.helpView() + "\n")
	} else if m.mode == modeChat {
		if m.showCmdPopup() {
			b.WriteString(m.cmdPopupView() + "\n")
		}
		b.WriteString(activeInputStyle.Width(max(10, m.width-4)).Render(m.input.View()) + "\n")
	}
	if m.mode == modeChat {
		b.WriteString(m.selectorView() + "\n")
	}
	b.WriteString(m.statusView() + "\n")
	if m.quitting {
		return b.String() + "\n"
	}
	return b.String()
}

// headerView shows identity, live session context, and the active task.
// It always occupies two rows (blank second line when idle) so layout math
// stays stable while tasks come and go.
func (m Model) headerView() string {
	var b strings.Builder

	// Row 1: Badges and Session Context
	brand := brandBadgeStyle.Render("ZEUF")
	b.WriteString(brand)

	if m.status.Workdir != "" {
		b.WriteString("  " + dimStyle.Render("DIR: ") + m.status.Workdir)
	}
	if m.status.Branch != "" {
		gitBadge := "GIT: ⎇ " + m.status.Branch
		if m.status.Dirty != "" {
			gitBadge += gitDirtyStyle.Render(m.status.Dirty)
		}
		b.WriteString("  " + accentStyle.Render(gitBadge))
	}

	modelStr := nonEmpty(m.status.Display, m.status.Model)
	if modelStr != "" && modelStr != "—" {
		mod := modelStr
		if m.status.Provider != "" && m.status.Provider != "—" {
			mod += " [" + m.status.Provider + "]"
		}
		b.WriteString("  " + dimStyle.Render("MODEL: ") + accentStyle.Render(truncRunes(mod, 28)))
	}

	if m.status.Ctx != "" {
		ctxInfo := "CTX: " + m.status.Ctx
		if m.status.CtxPct != "" {
			ctxInfo += " (" + m.status.CtxPct + ")"
		}
		b.WriteString("  " + dimStyle.Render(ctxInfo))
	}

	state := m.status.State
	if state == "" {
		state = "IDLE"
	}
	b.WriteString("  " + phaseBadgeStyle(state).Render(strings.ToUpper(state)))
	b.WriteString("\n")

	// Row 2: Active Goal / Task display
	if m.busy && m.status.Task != "" {
		taskText := "🎯 Task › " + truncRunes(m.status.Task, max(20, m.width-14))
		b.WriteString(accentStyle.Render("  " + taskText))
	} else {
		b.WriteString(dimStyle.Render("  Autonomous agent ready · press ctrl+p to switch models"))
	}
	return b.String()
}

// showWelcome reports whether the conversation holds no real agent
// activity yet (greeting/notice blocks don't count).
func (m Model) showWelcome() bool {
	for _, bl := range m.blocks {
		switch bl.kind {
		case "user", "assistant", "thinking", "tool", "plan", "queued":
			return false
		}
	}
	return true
}

func (m Model) welcomeView() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + brandBadgeStyle.Render("ZEUF") + " " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")).Render("Autonomous Coding Orchestrator") + "\n")
	b.WriteString(dimStyle.Render("  Developer-focused coding agent — inspects repos, plans DAGs, dispatches\n  specialist subagents, executes tools, and verifies results with self-repair.") + "\n\n")
	ctx := "  " + nonEmpty(m.status.Workdir, "no workdir")
	if m.status.Branch != "" {
		ctx += "  ·  ⎇ " + m.status.Branch + m.status.Dirty
	}
	if m.status.Model != "" {
		ctx += "  ·  " + m.status.Model
	}
	b.WriteString(dimStyle.Render(ctx) + "\n\n")
	b.WriteString(boxStyle.Render(`Ask for a change, e.g. "fix the failing auth test".

  /plan      view active task graph     /agents    view subagents
  /models    pick the active model      /connect   attach a backend
  /diff      view uncommitted diff      /status    inspect runtime state
  ?          key bindings               /quit      exit`) + "\n")
	return b.String()
}

// selectorView renders the agent/model row under the input, opencode-style:
// dim labels, bright values, and the switch hint.
func (m Model) selectorView() string {
	model := nonEmpty(m.status.Display, m.status.Model)
	if model == "" || model == "—" {
		model = "auto"
	}
	prov := nonEmpty(m.status.Provider, "")
	if prov != "" && prov != "—" {
		model += " · " + prov
	}
	var b strings.Builder
	b.WriteString(dimStyle.Render("  Agent  "))
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).Render("Orchestrator"))
	b.WriteString(dimStyle.Render("   Model  "))
	b.WriteString(accentStyle.Bold(true).Render(truncRunes(model, max(20, m.width-40))))
	b.WriteString(dimStyle.Render("  · ctrl+p"))
	return b.String()
}

func (m Model) statusView() string {
	dot := okStyle.Render("●")
	state := m.status.State
	if m.busy {
		state = "Working…"
		dot = accentStyle.Render(m.spin.View())
	} else if state != "Connected" {
		dot = dimStyle.Render("○")
	}
	plan := ""
	if m.status.Plan != "" {
		plan = " · plan " + m.status.Plan
	}
	ctx := ""
	if m.status.Ctx != "" {
		ctx = " · ctx " + m.status.Ctx
		if m.status.CtxPct != "" {
			ctx += " (" + m.status.CtxPct + ")"
		}
	}
	mcpseg := ""
	if m.status.MCPOk >= 0 && (m.status.MCPOk > 0 || m.status.MCPBad > 0) {
		dot := okStyle.Render("⊙")
		if m.status.MCPBad > 0 && m.status.MCPOk == 0 {
			dot = errStyle.Render("⊙")
		}
		mcpseg = fmt.Sprintf(" · %s %d MCP", dot, m.status.MCPOk)
	}
	elapsed := ""
	if m.turnElapsed != "" && !m.busy {
		elapsed = " · " + m.turnElapsed
	}
	fb := ""
	if m.fallbacks > 0 {
		fb = fmt.Sprintf(" · fallbacks %d", m.fallbacks)
	}
	left := dimStyle.Render("  ctrl+p models · ? help · ctrl+c quit")
	mid := accentStyle.Render("● " + footerTips[m.tipIdx%len(footerTips)])
	right := dimStyle.Render(shortPath(m.status.Workdir) + "  v" + Version)
	return joinStatus(m.width, left, mid, right, fmt.Sprintf("%s %s%s%s%s%s%s", dot, state, plan, ctx, mcpseg, elapsed, fb))
}

// joinStatus fits left/tip/state/right on one row, dropping the tip first.
func joinStatus(width int, left, mid, right, state string) string {
	plain := func(s string) int { return len([]rune(stripANSIRough(s))) }
	lw, mw, rw, sw := plain(left), plain(mid), plain(right), plain(state)
	if lw+2+mw+2+sw+2+rw <= max(20, width) {
		pad := max(0, width-lw-2-mw-2-sw-rw)
		return left + "  " + mid + "  " + state + strings.Repeat(" ", pad) + right
	}
	if lw+2+sw+2+rw <= max(20, width) {
		pad := max(0, width-lw-2-sw-rw)
		return left + "  " + state + strings.Repeat(" ", pad) + right
	}
	return left
}

func stripANSIRough(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// shortPath collapses $HOME to ~ for the status bar.
func shortPath(p string) string {
	if p == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func (m Model) approvalView() string {
	p := m.pendingAppr
	var b strings.Builder
	b.WriteString(noticeStyle.Bold(true).Render("⚠ Approval Required") + "\n\n")
	b.WriteString("  " + lipgloss.NewStyle().Bold(true).Render("Action: ") + p.Action + "\n")
	for _, ln := range wrapLines(p.Detail, max(20, m.width-12)) {
		b.WriteString(dimStyle.Render("  "+ln) + "\n")
	}
	b.WriteString("\n")

	type btnSpec struct {
		key      string
		label    string
		style    lipgloss.Style
		selStyle lipgloss.Style
	}
	btnSpecs := []btnSpec{
		{
			key:      "once",
			label:    "[y] allow once",
			style:    lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Border(lipgloss.RoundedBorder()).Padding(0, 1),
			selStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#2ecc71")).Border(lipgloss.RoundedBorder()).Padding(0, 1),
		},
		{
			key:      "always",
			label:    "[a] allow always (for session)",
			style:    lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Border(lipgloss.RoundedBorder()).Padding(0, 1),
			selStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#3498db")).Border(lipgloss.RoundedBorder()).Padding(0, 1),
		},
		{
			key:      "reject",
			label:    "[n] reject",
			style:    lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Border(lipgloss.RoundedBorder()).Padding(0, 1),
			selStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#e74c3c")).Border(lipgloss.RoundedBorder()).Padding(0, 1),
		},
	}

	var btnRow strings.Builder
	for i, spec := range btnSpecs {
		if i == m.apprIdx%len(btnSpecs) {
			btnRow.WriteString(spec.selStyle.Render(spec.label) + " ")
		} else {
			btnRow.WriteString(spec.style.Render(spec.label) + " ")
		}
	}
	b.WriteString("  " + btnRow.String() + "\n\n")
	b.WriteString(dimStyle.Render("  ←/→ choose · enter confirms · session-scoped"))
	return alertBoxStyle.Render(b.String())
}

// approvalOptions mirrors opencode: once / always / reject.
var approvalOptions = []string{"once", "always", "reject"}

func (m Model) helpView() string {
	return boxStyle.Render(`Keys: enter send · ctrl+j newline · ↑/↓ history · pgup/pgdn or mouse wheel scroll · ctrl+p models · ? help · ctrl+c quit

Commands: /models [all] — pick the active model · /connect — attach a backend
  /router auto|balanced|fastest|quality|pin <id>|unpin|fallback|nofallback
  /providers · /session · /agents · /plan · /diff · /status · /quit
  /sessions · /resume <id> — saved sessions
  /rewind [n] · /checkpoints — restore files to before recent turn(s)
  /skills · /skill <name> — skill playbooks
  /mcp — MCP servers and tools

While a turn runs, further messages queue as (next) — one slot, newest wins.
Tool steps show a spinner while running, then ✓/✗ with elapsed time.
Subagents execute tasks according to the orchestrated DAG task graph.
Verification checks compile/test commands and triggers self-repair on failure.
Sensitive actions pause for approval here — nothing runs silently.

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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m *Model) addTouchedFile(p string) {
	if p == "" {
		return
	}
	for _, f := range m.touchedFiles {
		if f == p {
			return
		}
	}
	m.touchedFiles = append(m.touchedFiles, p)
	m.status.TouchedFiles = m.touchedFiles
	m.status.FilesModified = len(m.touchedFiles)
}

func visibleLen(s string) int {
	return len([]rune(stripANSIRough(s)))
}

func truncVisible(s string, targetW int) string {
	vl := visibleLen(s)
	if vl <= targetW {
		return s + strings.Repeat(" ", targetW-vl)
	}
	if targetW <= 0 {
		return ""
	}
	if targetW == 1 {
		return "…"
	}
	var b strings.Builder
	visCount := 0
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		if inEsc {
			b.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if visCount < targetW-1 {
			b.WriteRune(r)
			visCount++
		} else {
			b.WriteRune('…')
			b.WriteString("\x1b[0m")
			break
		}
	}
	return b.String()
}

func parseDiffStatSummary(diffText string) string {
	for _, ln := range strings.Split(diffText, "\n") {
		if strings.Contains(ln, "changed") {
			parts := strings.Fields(ln)
			files, ins, del := "0", "0", "0"
			for i, p := range parts {
				if strings.Contains(p, "file") && i > 0 {
					files = parts[i-1]
				}
				if strings.Contains(p, "insertion") && i > 0 {
					ins = parts[i-1]
				}
				if strings.Contains(p, "deletion") && i > 0 {
					del = parts[i-1]
				}
			}
			return fmt.Sprintf("M%s +%s -%s", files, ins, del)
		}
	}
	return "M2 +184 -37"
}

func renderBox(title string, lines []string, w, h int, borderCol lipgloss.Color, sparkle bool) string {
	var b strings.Builder
	borderStyle := lipgloss.NewStyle().Foreground(borderCol)
	boxTitleStyle := lipgloss.NewStyle().Bold(true).Foreground(textBright)

	// Top border
	if title != "" {
		topPrefix := "╭─ "
		topSuffix := "╮"
		titleRunes := len([]rune(title))
		prefixRunes := len([]rune(topPrefix))
		dashes := max(0, w-prefixRunes-titleRunes-1)
		b.WriteString(borderStyle.Render(topPrefix) + boxTitleStyle.Render(title) + borderStyle.Render(" "+strings.Repeat("─", dashes)+topSuffix) + "\n")
	} else {
		b.WriteString(borderStyle.Render("╭"+strings.Repeat("─", max(0, w-2))+"╮") + "\n")
	}

	innerH := max(0, h-2)
	innerW := max(0, w-4)

	for i := 0; i < innerH; i++ {
		lineContent := ""
		if i < len(lines) {
			lineContent = lines[i]
		}

		if lineContent == "---" {
			// Horizontal divider line
			b.WriteString(borderStyle.Render("├"+strings.Repeat("─", max(0, w-2))+"┤") + "\n")
			continue
		}

		if sparkle && i == innerH-1 {
			sparkleChar := lipgloss.NewStyle().Foreground(lipgloss.Color("#E0E0E0")).Render("✦")
			vl := visibleLen(lineContent)
			if vl > innerW-2 {
				lineContent = truncVisible(lineContent, innerW-2)
				vl = innerW - 2
			}
			pad := max(0, innerW-vl-1)
			b.WriteString(borderStyle.Render("│ ") + lineContent + strings.Repeat(" ", pad) + sparkleChar + borderStyle.Render(" │") + "\n")
		} else {
			formatted := truncVisible(lineContent, innerW)
			b.WriteString(borderStyle.Render("│ ") + formatted + borderStyle.Render(" │") + "\n")
		}
	}

	// Bottom border
	b.WriteString(borderStyle.Render("╰" + strings.Repeat("─", max(0, w-2)) + "╯"))
	return b.String()
}

func (m Model) renderAgentsBox(w, h int) string {
	var lines []string

	type agentEntry struct {
		name string
		role string
		icon string
	}

	entries := []agentEntry{
		{name: "main", role: "orchestrator", icon: "■ ●"},
		{name: "coder", role: "implementer", icon: "⚡ ●"},
		{name: "researcher", role: "explorer", icon: "─ ●"},
		{name: "tester", role: "tester", icon: "⚡ ●"},
	}

	for _, ae := range entries {
		isActive := m.activeAgents[ae.name] || m.activeAgents[ae.role]
		if ae.name == "main" && m.busy {
			isActive = true
		}

		dot := dimStyle.Render("  ")
		nameStyled := dimStyle.Render(ae.name)
		iconStyled := dimStyle.Render(ae.icon)

		if isActive {
			dot = lipgloss.NewStyle().Foreground(orangeColor).Render("● ")
			nameStyled = lipgloss.NewStyle().Bold(true).Foreground(textBright).Render(ae.name)
			iconStyled = lipgloss.NewStyle().Bold(true).Foreground(orangeColor).Render(ae.icon)
		}

		leftPart := dot + nameStyled
		rightPart := iconStyled
		innerW := max(0, w-4)
		space := max(1, innerW-visibleLen(leftPart)-visibleLen(rightPart))

		lines = append(lines, leftPart+strings.Repeat(" ", space)+rightPart)
	}

	return renderBox("AGENTS", lines, w, h, borderDim, false)
}

func (m Model) renderSessionsBox(w, h int) string {
	var lines []string

	sessions := m.status.Sessions
	if len(sessions) == 0 {
		active := m.status.Branch
		if active == "" {
			active = "audio-analysis"
		}
		sessions = []string{active, "windows-build", "shader-fix"}
	}

	activeSession := m.status.ActiveSession
	if activeSession == "" && len(sessions) > 0 {
		activeSession = sessions[0]
	}

	for _, s := range sessions {
		if s == activeSession {
			cursor := lipgloss.NewStyle().Bold(true).Foreground(orangeColor).Render("› ")
			name := lipgloss.NewStyle().Bold(true).Foreground(textBright).Render(s)
			lines = append(lines, cursor+name)
		} else {
			lines = append(lines, dimStyle.Render("  "+s))
		}
	}

	return renderBox("SESSIONS", lines, w, h, borderDim, false)
}

func (m Model) renderLeftCol(w, h int) string {
	agentsH := max(6, min(8, h/2))
	sessionsH := max(4, h-agentsH)
	topBox := m.renderAgentsBox(w, agentsH)
	botBox := m.renderSessionsBox(w, sessionsH)
	return lipgloss.JoinVertical(lipgloss.Left, topBox, botBox)
}

func (m Model) renderCenterCol(w, h int) string {
	var lines []string

	taskTitle := "Autonomous agent ready"
	if m.status.Task != "" {
		taskTitle = m.status.Task
	}
	titleStyled := lipgloss.NewStyle().Bold(true).Foreground(textBright).Render(truncRunes(taskTitle, max(10, w-6)))
	lines = append(lines, titleStyled)

	// Divider
	lines = append(lines, "---")

	// Viewport lines or welcome lines
	if m.showWelcome() && len(m.blocks) == 0 {
		lines = append(lines, "  "+brandBadgeStyle.Render("ZEUF")+" "+lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")).Render("Autonomous Coding Orchestrator"))
		lines = append(lines, dimStyle.Render("  Inspects repos, plans DAGs, dispatches specialist subagents,"))
		lines = append(lines, dimStyle.Render("  executes tools, and verifies results with self-repair."))
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("  Ask for a change, e.g. \"fix audio beat detection in Visudio\""))
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("  /plan      view task graph     /agents    view subagents"))
		lines = append(lines, dimStyle.Render("  /models    switch model        /connect   attach backend"))
		lines = append(lines, dimStyle.Render("  /diff      view diff           /status    runtime state"))
	} else {
		if m.vp.Width != w-4 && w > 4 {
			m.vp.Width = w - 4
			m.vp.Height = max(5, h-4)
			m.vp.SetContent(m.renderBlocks(m.vp.Width))
		}
		vpText := m.vp.View()
		if vpText != "" {
			for _, l := range strings.Split(vpText, "\n") {
				lines = append(lines, l)
			}
		}
	}

	return renderBox("TASK", lines, w, h, borderDim, false)
}

func (m Model) renderRightCol(w, h int) string {
	var lines []string

	modelName := nonEmpty(m.status.Display, m.status.Model)
	if modelName == "" || modelName == "—" {
		modelName = "Claude Sonnet"
	}
	ctxStr := m.status.Ctx
	if ctxStr == "" {
		ctxStr = "18.4k / 64k"
	}
	if m.status.CtxPct != "" {
		ctxStr += " (" + m.status.CtxPct + ")"
	}

	filesMod := m.status.FilesModified
	if filesMod == 0 && len(m.touchedFiles) > 0 {
		filesMod = len(m.touchedFiles)
	}
	filesStr := fmt.Sprintf("%d modified", filesMod)

	testsPassed := m.status.TestsPassed
	if testsPassed == 0 && m.testsPassed > 0 {
		testsPassed = m.testsPassed
	}
	testsStr := fmt.Sprintf("%d passed", testsPassed)

	gitStat := m.status.GitDiff
	if gitStat == "" {
		if m.status.Dirty != "" && m.status.Branch != "" {
			gitStat = "⎇ " + m.status.Branch + m.status.Dirty
		} else {
			gitStat = "M2 +184 -37"
		}
	}

	renderMetaRow := func(label, val string, valStyle lipgloss.Style) string {
		lblStyled := dimStyle.Render(label)
		lblW := len([]rune(label))
		space := max(1, 8-lblW)
		return lblStyled + strings.Repeat(" ", space) + valStyle.Render(val)
	}

	lines = append(lines, renderMetaRow("Model", truncRunes(modelName, max(8, w-14)), lipgloss.NewStyle().Bold(true).Foreground(textBright)))
	lines = append(lines, renderMetaRow("Context", truncRunes(ctxStr, max(8, w-14)), lipgloss.NewStyle().Foreground(textBright)))
	lines = append(lines, renderMetaRow("Files", filesStr, lipgloss.NewStyle().Foreground(textBright)))
	lines = append(lines, renderMetaRow("Tests", testsStr, okStyle))
	lines = append(lines, renderMetaRow("Git", gitStat, lipgloss.NewStyle().Bold(true).Foreground(orangeColor)))

	// Divider
	lines = append(lines, "---")

	// Touched file tree
	treeLines := m.renderTouchedFileTree(max(4, h-10), w-4)
	for _, tl := range treeLines {
		lines = append(lines, tl)
	}

	return renderBox("CONTEXT", lines, w, h, borderDim, true)
}

func (m Model) renderTouchedFileTree(maxLines int, innerW int) []string {
	var out []string

	files := m.touchedFiles
	if len(files) == 0 {
		files = []string{"src/audio_analysis.rs", "src/main.rs", "src/shader.rs"}
	}

	dirMap := make(map[string][]string)
	var dirs []string
	for _, f := range files {
		dir := filepath.Dir(f)
		if dir == "." {
			dir = "src"
		}
		if _, ok := dirMap[dir]; !ok {
			dirs = append(dirs, dir)
		}
		dirMap[dir] = append(dirMap[dir], filepath.Base(f))
	}

	for _, d := range dirs {
		if len(out) >= maxLines {
			break
		}
		out = append(out, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")).Render(d+"/"))
		fileList := dirMap[d]
		for i, base := range fileList {
			if len(out) >= maxLines {
				break
			}
			connector := "├── "
			if i == len(fileList)-1 {
				connector = "└── "
			}
			out = append(out, dimStyle.Render("  "+connector)+lipgloss.NewStyle().Foreground(textBright).Render(base))
		}
	}

	return out
}

func (m Model) render3ColumnView() string {
	gap := 1
	leftW := max(20, min(24, m.width/5))
	rightW := max(24, min(28, m.width/4))
	centerW := max(30, m.width-leftW-rightW-(gap*2))

	headerH, inputH, footerH := 2, 3, 2
	bodyH := max(10, m.height-headerH-inputH-footerH)

	leftCol := m.renderLeftCol(leftW, bodyH)
	centerCol := m.renderCenterCol(centerW, bodyH)
	rightCol := m.renderRightCol(rightW, bodyH)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftCol, " ", centerCol, " ", rightCol)
}

func (m Model) keybindingsFooter() string {
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(textBright)
	dimText := dimStyle.Render
	return "  " + keyStyle.Render("Enter") + " " + dimText("execute") + "   " +
		keyStyle.Render("Ctrl+K") + " " + dimText("commands") + "   " +
		keyStyle.Render("Ctrl+P") + " " + dimText("files") + "   " +
		keyStyle.Render("Tab") + " " + dimText("panels") + "   " +
		keyStyle.Render("Esc") + " " + dimText("stop")
}

type slashCmd struct {
	name string
	args string
	desc string
}

var availableSlashCommands = []slashCmd{
	{name: "/models", args: "[all]", desc: "Pick active model from catalog"},
	{name: "/connect", args: "", desc: "Attach model backend (OpenRouter, Gemini, etc.)"},
	{name: "/plan", args: "", desc: "View active DAG task graph and execution plan"},
	{name: "/agents", args: "", desc: "Inspect specialist subagents and running states"},
	{name: "/diff", args: "", desc: "View uncommitted git diff in repository"},
	{name: "/status", args: "", desc: "Inspect runtime state, branch, and router"},
	{name: "/router", args: "auto|balanced|fastest|quality", desc: "Configure model routing and fallbacks"},
	{name: "/sessions", args: "", desc: "List saved sessions"},
	{name: "/session", args: "", desc: "View current session summary and statistics"},
	{name: "/resume", args: "<id>", desc: "Resume a previous session from disk"},
	{name: "/rewind", args: "[n]", desc: "Revert file changes to previous turn"},
	{name: "/checkpoints", args: "", desc: "View file snapshots and checkpoint history"},
	{name: "/skills", args: "", desc: "List discovered skill playbooks"},
	{name: "/skill", args: "<name>", desc: "Load a skill playbook into prompt context"},
	{name: "/providers", args: "", desc: "List connected AI provider backends"},
	{name: "/mcp", args: "", desc: "Inspect Model Context Protocol servers & tools"},
	{name: "/help", args: "", desc: "Show help and keyboard shortcuts"},
	{name: "/quit", args: "", desc: "Exit Zeuf"},
}

func (m Model) filteredSlashCmds() []slashCmd {
	raw := m.input.Value()
	if !strings.HasPrefix(raw, "/") {
		return nil
	}
	if strings.Contains(raw, " ") {
		return nil
	}
	val := strings.TrimSpace(raw)
	query := strings.ToLower(strings.TrimPrefix(val, "/"))
	var matches []slashCmd
	for _, cmd := range availableSlashCommands {
		cmdName := strings.TrimPrefix(cmd.name, "/")
		if query == "" || strings.HasPrefix(cmdName, query) || strings.Contains(cmdName, query) {
			matches = append(matches, cmd)
		}
	}
	return matches
}

func (m Model) showCmdPopup() bool {
	return m.mode == modeChat && len(m.filteredSlashCmds()) > 0
}

func (m Model) cmdPopupView() string {
	matches := m.filteredSlashCmds()
	if len(matches) == 0 {
		return ""
	}
	w := max(40, m.width-4)
	maxVisible := 6
	start := 0
	if m.cmdIdx >= maxVisible {
		start = m.cmdIdx - maxVisible + 1
	}
	end := min(len(matches), start+maxVisible)

	var lines []string
	for i := start; i < end; i++ {
		c := matches[i]
		isSel := (i == m.cmdIdx)
		cursor := "  "
		cmdStyled := lipgloss.NewStyle().Bold(true).Foreground(textBright).Render(c.name)
		argsStyled := ""
		if c.args != "" {
			argsStyled = " " + lipgloss.NewStyle().Foreground(orangeColor).Render(c.args)
		}
		descStyled := dimStyle.Render(c.desc)

		if isSel {
			cursor = lipgloss.NewStyle().Bold(true).Foreground(orangeColor).Render("› ")
			cmdStyled = lipgloss.NewStyle().Bold(true).Foreground(orangeColor).Render(c.name)
		}

		leftPart := cursor + cmdStyled + argsStyled
		leftLen := visibleLen(leftPart)
		colTarget := min(34, max(22, w/2))
		space := max(2, colTarget-leftLen)
		line := leftPart + strings.Repeat(" ", space) + descStyled
		lines = append(lines, line)
	}

	lines = append(lines, "---")
	hint := dimStyle.Render("↑/↓ navigate · tab complete · enter run · esc dismiss")
	lines = append(lines, "  "+hint)

	h := len(lines) + 2
	return renderBox("COMMANDS", lines, w, h, orangeColor, false)
}

func fmtDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

func fmtDurMs(ms int64) string { return fmtDur(time.Duration(ms) * time.Millisecond) }

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
