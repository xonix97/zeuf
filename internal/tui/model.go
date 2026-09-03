package tui

import (
	"encoding/json"
	"fmt"
	"os"
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
	kind string // "user","assistant","thinking","tool","plan","system","notice","error"
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
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("99")).Padding(1, 2)
)

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
	ta.Placeholder = "Ask Zeuf to inspect, edit, build, fix…  (/models · /connect · ? help)"
	ta.ShowLineNumbers = false
	ta.SetHeight(3)
	ta.Focus()
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = accentStyle
	return Model{
		events:    events,
		submit:    submit,
		actions:   actions,
		input:     ta,
		spin:      sp,
		status:    Status{State: "Starting", MCPOk: -1},
		histIdx:   -1,
		streamIdx: -1,
		follow:    true,
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
		headerH, footerH := 2, 9
		m.vp = viewport.New(max(10, msg.Width-2), max(5, msg.Height-headerH-footerH))
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
	case "tool-end":
		m.completeTool(ev.Tool, ev.Text, ev.Ok, ev.Depth)
	case "switch":
		m.fallbacks++
		m.blocks = append(m.blocks, block{kind: "notice", text: "Model limit reached. Continuing with " + ev.Text + "."})
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
			if detail == "" {
				m.blocks = append(m.blocks[:i], m.blocks[i+1:]...)
			} else {
				m.blocks[i].text = detail
			}
			return
		}
	}
	if detail != "" {
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
		b.WriteString(m.renderPlan(bl))
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
	w := max(20, width-2)
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
	plain := bl.toolSummary + " · " + fmtDurMs(bl.toolMs)
	lines := wrapLines(plain, w-2)
	for i, ln := range lines {
		if i == 0 {
			// opencode-style step marker, colored by outcome.
			mark := okStyle.Render("↳")
			if !bl.toolOk {
				mark = errStyle.Render("↳")
			}
			b.WriteString(mark + " " + ln + "\n")
		} else {
			b.WriteString(dimStyle.Render("  "+ln) + "\n")
		}
	}
	for _, ln := range bl.writeLines {
		b.WriteString(dimStyle.Render("  "+ln) + "\n")
	}
	if bl.toolOut != "" {
		for _, ln := range wrapLines("⎿ "+bl.toolOut, w) {
			b.WriteString(dimStyle.Render(ln) + "\n")
		}
	}
	return b.String()
}

func (m *Model) renderPlan(bl block) string {
	var b strings.Builder
	done, total := 0, 0
	type step struct {
		done  bool
		title string
	}
	var steps []step
	for _, ln := range strings.Split(bl.text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		total++
		s := step{title: ln}
		if strings.HasPrefix(ln, "1:") {
			s.done = true
			s.title = strings.TrimSpace(strings.TrimPrefix(ln, "1:"))
			done++
		} else {
			s.title = strings.TrimSpace(strings.TrimPrefix(ln, "0:"))
		}
		steps = append(steps, s)
	}
	b.WriteString(dimStyle.Render(fmt.Sprintf("Plan %d/%d", done, total)) + "\n")
	current := true
	for _, s := range steps {
		switch {
		case s.done:
			b.WriteString(okStyle.Render("  ✓ "+s.title) + "\n")
		case current:
			b.WriteString(accentStyle.Render("  ● "+s.title) + "\n")
			current = false
		default:
			b.WriteString(dimStyle.Render("  ○ "+s.title) + "\n")
		}
	}
	return b.String()
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
		m.busy = true
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
	if m.showWelcome() && m.mode == modeChat {
		b.WriteString(m.welcomeView())
	} else {
		switch m.mode {
		case modePicker:
			b.WriteString(m.pickerView())
		case modeConnect:
			b.WriteString(m.connectView())
		default:
			b.WriteString(m.vp.View() + "\n")
		}
	}
	if m.mode == modeApproval && m.pendingAppr != nil {
		b.WriteString(m.approvalView() + "\n")
	} else if m.mode == modeHelp {
		b.WriteString(m.helpView() + "\n")
	} else if m.mode == modeChat {
		b.WriteString(boxStyle.Render(m.input.View()) + "\n")
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
	b.WriteString(titleStyle.Render("✦ Zeuf"))
	if m.status.Workdir != "" {
		b.WriteString(dimStyle.Render("  " + m.status.Workdir))
	}
	if m.status.Branch != "" {
		b.WriteString(accentStyle.Render("  ⎇ " + m.status.Branch + m.status.Dirty))
	}
	b.WriteString("\n")
	if m.busy && m.status.Task != "" {
		b.WriteString(dimStyle.Render("  › " + truncRunes(m.status.Task, max(20, m.width-6))))
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
	b.WriteString(titleStyle.Render("  ✦ Zeuf") + "\n")
	b.WriteString(dimStyle.Render("  Your coding agent — inspect, edit, build, and fix, with routing\n  and fallback across your model backends.") + "\n\n")
	ctx := "  " + nonEmpty(m.status.Workdir, "no workdir")
	if m.status.Branch != "" {
		ctx += "  ·  ⎇ " + m.status.Branch + m.status.Dirty
	}
	if m.status.Model != "" {
		ctx += "  ·  " + m.status.Model
	}
	b.WriteString(dimStyle.Render(ctx) + "\n\n")
	b.WriteString(boxStyle.Render(`Ask for a change, e.g. "fix the failing auth test".

  /models    pick the active model      /connect   attach a backend
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
	b.WriteString("Orchestrator")
	b.WriteString(dimStyle.Render("   Model  "))
	b.WriteString(accentStyle.Render(truncRunes(model, max(20, m.width-40))))
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
	b.WriteString(titleStyle.Render("Approval required") + "\n\n")
	b.WriteString("  " + p.Action + "\n")
	for _, ln := range wrapLines(p.Detail, max(20, m.width-12)) {
		b.WriteString(dimStyle.Render("  "+ln) + "\n")
	}
	b.WriteString("\n")
	labels := map[string]string{"once": "[y] allow once", "always": "[a] allow always", "reject": "[n] reject"}
	for i, opt := range approvalOptions {
		label := "  " + labels[opt] + "  "
		if i == m.apprIdx%len(approvalOptions) {
			b.WriteString(okStyle.Render(label))
		} else {
			b.WriteString(dimStyle.Render(label))
		}
	}
	b.WriteString(dimStyle.Render("\n\n  ←/→ choose · enter confirms · session-scoped"))
	return boxStyle.Render(b.String())
}

// approvalOptions mirrors opencode: once / always / reject.
var approvalOptions = []string{"once", "always", "reject"}

func (m Model) helpView() string {
	return boxStyle.Render(`Keys: enter send · ctrl+j newline · ↑/↓ history · pgup/pgdn or mouse wheel scroll · ctrl+p models · ? help · ctrl+c quit

Commands: /models [all] — pick the active model · /connect — attach a backend
  /router auto|balanced|fastest|quality|pin <id>|unpin|fallback|nofallback
  /providers · /session · /agents · /quit
  /sessions · /resume <id> — saved sessions
  /rewind [n] · /checkpoints — restore files to before recent turn(s)
  /skills · /skill <name> — skill playbooks
  /mcp — MCP servers and tools

While a turn runs, further messages queue as (next) — one slot, newest wins.
Tool steps show a spinner while running, then ✓/✗ with elapsed time.
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
