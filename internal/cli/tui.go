package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"zeuf/internal/agent"
	"zeuf/internal/config"
	"zeuf/internal/core"
	ctools "zeuf/internal/core/tools"
	"zeuf/internal/mcp"
	"zeuf/internal/providers/anthropic"
	"zeuf/internal/providers/direct"
	"zeuf/internal/router"
	"zeuf/internal/skills"
	"zeuf/internal/tui"
)

func tuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Start the full-screen terminal interface",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd.Context())
		},
	}
}

// runTUI boots discovery, then runs the agent core behind the bubbletea
// client. The TUI never implements agent logic: input lines become tasks,
// core events become conversation/tool/switch/status rendering, and UI
// actions (pin/connect) come back over the actions channel.
func runTUI(ctx context.Context) error {
	cfg, reg, r, tools, ag, err := session(ctx, "")
	if err != nil {
		return err
	}
	prefs := prefsFrom(cfg)
	refreshNow(ctx, reg)
	mgr := attachMCP(ctx, cfg, tools)
	defer mgr.Close()

	hub := agent.NewHub()
	ag.Hub = hub

	events := make(chan tui.Event, 512)
	submit := make(chan string, 16)
	actions := make(chan tui.Action, 16)
	m := tui.NewFull(events, submit, actions)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sess := agent.NewSession(newID(), "", tools)

	// Approvals: agent loop → modal. (Never stdin: bubbletea owns the TTY.)
	go func() {
		for req := range hub.Requests {
			select {
			case events <- tui.Event{Kind: "approval", Approval: req}:
			case <-ctx.Done():
				return
			}
		}
	}()

	r.OnSwitch = func(s router.SwitchInfo) {
		events <- tui.Event{Kind: "switch", Text: s.To, Detail: s.From + "|" + s.Reason}
		events <- tui.Event{Kind: "status", Text: statusFor(reg, s.To)}
	}
	ag.Emit = func(ev agent.Event) {
		switch ev.Type {
		case agent.EvPhase:
			events <- tui.Event{Kind: "phase", Text: ev.Phase, Detail: ev.Text}
		case agent.EvGraph:
			events <- tui.Event{Kind: "plan", Text: planCounts(sess), Detail: planDetail(sess), Graph: ev.Graph}
			events <- tui.Event{Kind: "session", Detail: sessionDetail(tools)}
		case agent.EvSubStart:
			events <- tui.Event{Kind: "agent-start", Role: ev.Role, TaskID: ev.TaskID, Text: ev.Text, Depth: 1}
		case agent.EvSubEnd:
			events <- tui.Event{Kind: "agent-end", Role: ev.Role, TaskID: ev.TaskID, Text: ev.Text, Ok: ev.Ok, Duration: ev.Duration, Depth: 1}
		case agent.EvVerifyStart:
			events <- tui.Event{Kind: "verify-start", Text: ev.Text}
		case agent.EvVerifyEnd:
			events <- tui.Event{Kind: "verify-end", Text: ev.Text, Ok: ev.Ok, Duration: ev.Duration, Detail: ev.Diagnosis}
		case agent.EvDiff:
			if ev.DiffStat != "" {
				events <- tui.Event{Kind: "diff", Text: ev.DiffStat}
			}
		case agent.EvToken:
			events <- tui.Event{Kind: "token", Text: ev.Text, Depth: ev.Depth}
		case agent.EvReasoning:
			events <- tui.Event{Kind: "reasoning", Text: ev.Text, Depth: ev.Depth}
		case agent.EvUsage:
			events <- usageEvent(reg, sess, ev.Model)
		case agent.EvToolStart:
			events <- tui.Event{Kind: "tool-start", Tool: ev.Tool, Args: ev.Text, Depth: ev.Depth}
		case agent.EvToolEnd:
			events <- tui.Event{Kind: "tool-end", Tool: ev.Tool, Text: toolPreview(ev.Text), Ok: ev.Ok, Depth: ev.Depth}
			events <- tui.Event{Kind: "plan", Text: planCounts(sess), Detail: planDetail(sess)}
			events <- tui.Event{Kind: "session", Detail: sessionDetail(tools)}
		case agent.EvAssistant:
			events <- tui.Event{Kind: "text", Text: ev.Text, Depth: ev.Depth}
		case agent.EvError:
			events <- tui.Event{Kind: "error", Text: core.Redact(ev.Text)}
		case agent.EvDone:
			events <- tui.Event{Kind: "done"}
		}
	}

	// Single core loop: submit lines and UI actions share it, so prefs and
	// the registry are never touched concurrently.
	go func() {
		events <- tui.Event{Kind: "status", Text: statusFor(reg, "")}
		events <- tui.Event{Kind: "session", Detail: sessionDetail(tools)}
		events <- tui.Event{Kind: "mcp", Text: fmt.Sprint(mcpHealthy(mgr)), Detail: fmt.Sprint(mcpFailed(mgr))}
		events <- tui.Event{Kind: "system", Text: fmt.Sprintf("Zeuf ready — %d free models across %d backends. Type a task, /quit to exit.", len(router.FreeOnly(reg.Models())), len(reg.Backends()))}
		for {
			select {
			case <-ctx.Done():
				return
			case line, ok := <-submit:
				if !ok {
					return
				}
				if handleTUILine(ctx, strings.TrimSpace(line), &prefs, reg, r, ag, sess, events, p, mgr) {
					return
				}
			case act, ok := <-actions:
				if !ok {
					return
				}
				handleTUIAction(ctx, act, &prefs, reg, events, hub)
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

// handleTUILine processes one submitted line. True means the UI should stop.
func handleTUILine(ctx context.Context, line string, prefs *router.Prefs, reg *router.Registry, r *router.Router, ag *agent.Agent, sess *agent.Session2, events chan tui.Event, p *tea.Program, mgr *mcp.Manager) bool {
	defer func() {
		if strings.HasPrefix(line, "/") && line != "/quit" && line != "/exit" {
			events <- tui.Event{Kind: "done"}
		}
	}()

	switch {
	case line == "":
		return false
	case line == "/quit" || line == "/exit":
		close(events)
		p.Quit()
		return true
	case line == "/connect":
		events <- tui.Event{Kind: "connect-open"}
		return false
	case line == "/models" || line == "/models all":
		events <- tui.Event{Kind: "picker", Models: pickerRows(reg, prefs, line != "/models all")}
		return false
	case line == "/help":
		events <- tui.Event{Kind: "text", Text: "Commands: /models [all] · /connect · /router … · /providers · /session · /agents · /sessions · /resume · /rewind · /skill · /mcp · /quit  (press ? anytime)"}
		return false
	case line == "/providers":
		events <- tui.Event{Kind: "text", Text: "Backends: " + strings.Join(reg.Backends(), ", ")}
		return false
	case line == "/agents":
		events <- tui.Event{Kind: "text", Text: formatAgents(ag.SnapshotSubs())}
		return false
	case line == "/sessions":
		var b strings.Builder
		list, err := core.ListSessions()
		if err != nil {
			events <- tui.Event{Kind: "error", Text: core.Redact(err.Error())}
			return false
		}
		if len(list) == 0 {
			b.WriteString("no saved sessions yet")
		}
		for _, s := range list {
			fmt.Fprintf(&b, "%s  %s  %d turn(s)  %s\n", s.ID, s.Updated.Format("2006-01-02 15:04"), s.Turns, truncStr(s.Task, 50))
		}
		events <- tui.Event{Kind: "text", Text: strings.TrimRight(b.String(), "\n")}
		return false
	case line == "/checkpoints":
		events <- tui.Event{Kind: "text", Text: formatCheckpoints(sess)}
		return false
	case line == "/skills":
		events <- tui.Event{Kind: "text", Text: formatSkills(skills.Discover(ag.Tools.Workdir))}
		return false
	case line == "/mcp":
		events <- tui.Event{Kind: "text", Text: formatMCP(mgr)}
		return false
	case strings.HasPrefix(line, "/resume"):
		parts := strings.Fields(line)
		if len(parts) < 2 {
			events <- tui.Event{Kind: "text", Text: "usage: /resume <id>  (see /sessions)"}
			return false
		}
		loaded, err := core.LoadSession(parts[1])
		if err != nil {
			events <- tui.Event{Kind: "error", Text: core.Redact(err.Error())}
			return false
		}
		sess.Session = loaded
		note := ""
		if wd, ok := loaded.Meta["workdir"]; ok && wd != "" && wd != ag.Tools.Workdir {
			note = fmt.Sprintf(" (was in %s, now in %s)", wd, ag.Tools.Workdir)
		}
		events <- tui.Event{Kind: "text", Text: fmt.Sprintf("Resumed %s (%d message(s), %d checkpoint(s))%s", loaded.ID, len(loaded.Messages), len(loaded.Checkpoints), note)}
		return false
	case strings.HasPrefix(line, "/rewind"):
		parts := strings.Fields(line)
		n := 1
		if len(parts) > 1 {
			fmt.Sscanf(parts[1], "%d", &n)
		}
		out, err := agent.Rewind(sess, ag.Tools, n)
		if err != nil {
			events <- tui.Event{Kind: "error", Text: core.Redact(err.Error())}
			return false
		}
		events <- tui.Event{Kind: "text", Text: strings.Join(out, "\n")}
		return false
	case strings.HasPrefix(line, "/skill"):
		parts := strings.Fields(line)
		if len(parts) < 2 {
			events <- tui.Event{Kind: "text", Text: "usage: /skill <name>  (see /skills)"}
			return false
		}
		s, ok := skills.Find(ag.Tools.Workdir, parts[1])
		if !ok {
			events <- tui.Event{Kind: "text", Text: fmt.Sprintf("skill %q not found (see /skills)", parts[1])}
			return false
		}
		sess.Session.AppendSystem("Skill \"" + s.Name + "\" loaded:\n" + s.Body)
		events <- tui.Event{Kind: "text", Text: fmt.Sprintf("Skill %s loaded into context.", s.Name)}
		return false
	case line == "/plan":
		if len(sess.Session.TaskGraphData) > 0 {
			if g, err := agent.FromJSON(sess.Session.TaskGraphData); err == nil {
				events <- tui.Event{Kind: "text", Text: g.Format()}
				return false
			}
		}
		events <- tui.Event{Kind: "text", Text: "Plan:\n" + planDetail(sess)}
		return false
	case line == "/diff":
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		diffOut, err := exec.CommandContext(cctx, "git", "-C", ag.Tools.Workdir, "diff", "--stat").Output()
		if err != nil || len(diffOut) == 0 {
			events <- tui.Event{Kind: "text", Text: "No uncommitted diff in repository."}
		} else {
			events <- tui.Event{Kind: "diff", Text: string(diffOut)}
		}
		return false
	case line == "/status":
		branch, dirty := ag.Tools.GitInfo()
		text := fmt.Sprintf("Workdir: %s\nGit: branch %s (dirty: %s)\nRouter: mode=%s pinned=%s fallback=%v", ag.Tools.Workdir, branch, dirty, prefs.Mode, prefs.PinnedModel, prefs.FallbackEnabled)
		events <- tui.Event{Kind: "text", Text: text}
		return false
	case line == "/session":
		events <- tui.Event{Kind: "text", Text: sess.Summary()}
		return false
	case strings.HasPrefix(line, "/router"):
		handleRouter(strings.Fields(line)[1:], prefs)
		events <- tui.Event{Kind: "text", Text: fmt.Sprintf("router: mode=%s pinned=%s fallback=%v", prefs.Mode, prefs.PinnedModel, prefs.FallbackEnabled)}
		events <- tui.Event{Kind: "status", Text: statusFor(reg, prefs.PinnedModel)}
		return false
	case strings.HasPrefix(line, "/"):
		events <- tui.Event{Kind: "text", Text: "unknown command. Try /help."}
		return false
	}
	sess.Session.Task = line
	sess.AppendUser(line)
	events <- tui.Event{Kind: "task", Text: line}
	if _, err := ag.RunTurn(ctx, sess, *prefs); err != nil {
		events <- tui.Event{Kind: "error", Text: core.Redact(err.Error())}
	}
	if saveErr := saveSession(sess, ag.Tools.Workdir); saveErr != nil {
		events <- tui.Event{Kind: "error", Text: "save session: " + core.Redact(saveErr.Error())}
	}
	return false
}

// handleTUIAction applies picker/wizard/modal outcomes from the TUI.
func handleTUIAction(ctx context.Context, act tui.Action, prefs *router.Prefs, reg *router.Registry, events chan tui.Event, hub *agent.Hub) {
	switch a := act.(type) {
	case tui.ActionAllowAlways:
		hub.AllowAlways(a.Tool)
	case tui.ActionOpenPicker:
		events <- tui.Event{Kind: "picker", Models: pickerRows(reg, prefs, true)}
	case tui.ActionPin:
		prefs.PinnedModel = a.FullID
		events <- tui.Event{Kind: "status", Text: statusFor(reg, a.FullID)}
		events <- tui.Event{Kind: "text", Text: "Pinned model: " + a.FullID + "  (/router unpin for automatic)"}
		events <- tui.Event{Kind: "done"}
	case tui.ActionUnpin:
		prefs.PinnedModel = ""
		events <- tui.Event{Kind: "status", Text: statusFor(reg, "")}
		events <- tui.Event{Kind: "text", Text: "Routing: automatic."}
		events <- tui.Event{Kind: "done"}
	case tui.ActionConnect:
		used, err := Save(ConnectSpec{Name: a.Name, BaseURL: a.BaseURL, KeyEnv: a.KeyEnv, Secret: a.Secret})
		if err != nil {
			events <- tui.Event{Kind: "error", Text: core.Redact(err.Error())}
			return
		}
		registerDirect(reg, a.Name)
		refreshNow(ctx, reg)
		events <- tui.Event{Kind: "status", Text: statusFor(reg, "")}
		events <- tui.Event{Kind: "text", Text: fmt.Sprintf("Connected %q (credential: %s). %d free models available.", a.Name, used, len(router.FreeOnly(reg.Models())))}
		events <- tui.Event{Kind: "done"}
	case tui.ActionLogin:
		refreshNow(ctx, reg)
		events <- tui.Event{Kind: "status", Text: statusFor(reg, "")}
		events <- tui.Event{Kind: "text", Text: fmt.Sprintf("Rescanned %s: %d free models available.", a.Backend, len(router.FreeOnly(reg.Models())))}
		events <- tui.Event{Kind: "done"}
	}
}

// formatAgents renders subagent records for /agents. Empty means the
// orchestrator did everything itself this session.
func formatAgents(subs []agent.SubInfo) string {
	if len(subs) == 0 {
		return "No subagents yet — the orchestrator handled everything itself."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Subagents (%d):\n", len(subs))
	for i, s := range subs {
		mark := "✓"
		if !s.Ok {
			mark = "✗"
		}
		fmt.Fprintf(&b, "%d. %s [%s · %s] %s\n   └ %s\n",
			i+1, mark, fmtDurAgent(s.Ms), nonEmptyStr(s.Model, "unknown model"),
			truncAgent(s.Task, 80), truncAgent(s.Summary, 160))
	}
	return strings.TrimRight(b.String(), "\n")
}

func fmtDurAgent(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

func truncAgent(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// usageEvent builds the context meter from session totals and the
// current model's window. Unknown windows or zero totals show honestly.
func usageEvent(reg *router.Registry, sess *agent.Session2, model string) tui.Event {
	used := sess.Session.TokensIn + sess.Session.TokensOut
	ctxLen := 0
	for _, e := range reg.Models() {
		if e.Model.FullID() == model || e.Model.ID == model {
			ctxLen = e.Model.Caps.ContextLength
			break
		}
	}
	if used <= 0 || ctxLen <= 0 {
		if used > 0 {
			return tui.Event{Kind: "usage", Text: fmtK(used) + "/?", Detail: ""}
		}
		return tui.Event{Kind: "usage", Text: "", Detail: ""}
	}
	pct := used * 100 / int64(ctxLen)
	return tui.Event{Kind: "usage", Text: fmtK(used) + "/" + fmtK(int64(ctxLen)), Detail: fmt.Sprintf("%d%%", pct)}
}

// fmtK condenses token counts: 999, 12.4k, 3.4M.
func fmtK(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		if n%1000 == 0 {
			return fmt.Sprintf("%dk", n/1000)
		}
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
}

// mcpHealthy/mcpFailed count manager backends for the footer segment.
func mcpHealthy(mgr *mcp.Manager) int {
	n := 0
	for _, st := range mgr.Status() {
		if st.OK {
			n++
		}
	}
	return n
}

func mcpFailed(mgr *mcp.Manager) int {
	n := 0
	for _, st := range mgr.Status() {
		if !st.OK {
			n++
		}
	}
	return n
}

// pickerRows builds /models rows (free only unless all).
func pickerRows(reg *router.Registry, prefs *router.Prefs, all bool) []tui.PickerModel {
	es := reg.Models()
	if !all {
		es = router.FreeOnly(es)
	}
	rows := make([]tui.PickerModel, 0, len(es))
	for _, e := range es {
		ctxLen := "ctx?"
		if e.Model.Caps.ContextLength > 0 {
			ctxLen = fmt.Sprintf("%dK ctx", e.Model.Caps.ContextLength/1000)
		}
		rows = append(rows, tui.PickerModel{
			FullID:  e.Model.FullID(),
			Display: nonEmptyStr(e.Model.DisplayName, e.Model.ID),
			Detail:  fmt.Sprintf("%s · %s · %s · %s", e.Backend.Name(), ctxLen, priceWord(e.Model), e.Model.Availability),
			Free:    e.Model.IsFree,
			Pinned:  prefs.PinnedModel == e.Model.FullID() || prefs.PinnedModel == e.Model.ID,
		})
	}
	return rows
}

func nonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func planCounts(sess *agent.Session2) string {
	done, total := 0, len(sess.Session.Plan)
	for _, p := range sess.Session.Plan {
		if p.Done {
			done++
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", done, total)
}

// planDetail renders plan steps as "1:title"/"0:title" lines for the TUI
// checklist. Empty when the agent has no plan yet.
func planDetail(sess *agent.Session2) string {
	var b strings.Builder
	for _, p := range sess.Session.Plan {
		mark := "0"
		if p.Done {
			mark = "1"
		}
		title := strings.ReplaceAll(strings.TrimSpace(p.Title), "\n", " ")
		fmt.Fprintf(&b, "%s:%s\n", mark, title)
	}
	return strings.TrimRight(b.String(), "\n")
}

// toolPreview condenses a tool result to one UI line (first meaningful
// line, bounded). Full output stays in session context, not on screen.
func toolPreview(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len([]rune(line)) > 140 {
			line = string([]rune(line)[:140]) + "…"
		}
		return line
	}
	return ""
}

// sessionDetail renders "workdir|branch|dirty" for the TUI header.
func sessionDetail(reg *ctools.Registry) string {
	branch, dirty := reg.GitInfo()
	return reg.Workdir + "|" + branch + "|" + dirty
}

// registerDirect adds a saved direct endpoint to the live registry.
func registerDirect(reg *router.Registry, name string) {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	for _, d := range cfg.Direct {
		if d.Name == name {
			if d.Type == "anthropic" || d.Name == "anthropic" || strings.Contains(d.BaseURL, "anthropic.com") {
				reg.Register(anthropic.New(anthropic.Config{Name: d.Name, BaseURL: d.BaseURL, APIKeyEnv: d.APIKeyEnv}))
			} else {
				reg.Register(direct.New(direct.Config{Name: d.Name, BaseURL: d.BaseURL, APIKeyEnv: d.APIKeyEnv}))
			}
			return
		}
	}
}

// statusFor renders "model|provider|state|display" for the TUI chrome.
func statusFor(reg *router.Registry, override string) string {
	models := reg.Models()
	if len(models) == 0 {
		return "—|—|Offline|—"
	}
	m := models[0]
	if override != "" {
		for _, e := range models {
			if e.Model.FullID() == override || e.Model.ID == override {
				m = e
				break
			}
		}
	}
	state := "Connected"
	if m.Model.Availability != "available" {
		state = "Degraded"
	}
	short := m.Model.ID
	if len(short) > 28 {
		short = short[:28] + "…"
	}
	display := m.Model.DisplayName
	if display == "" {
		display = m.Model.ID
	}
	if len([]rune(display)) > 26 {
		display = string([]rune(display)[:26]) + "…"
	}
	return short + "|" + m.Backend.Name() + "|" + state + "|" + display
}
