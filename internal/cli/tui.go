package cli

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"zeuf/internal/agent"
	"zeuf/internal/config"
	"zeuf/internal/core"
	"zeuf/internal/providers/direct"
	"zeuf/internal/router"
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

	hub := agent.NewHub()
	ag.Hub = hub

	events := make(chan tui.Event, 512)
	submit := make(chan string, 16)
	actions := make(chan tui.Action, 16)
	m := tui.NewFull(events, submit, actions)
	p := tea.NewProgram(m, tea.WithAltScreen())

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
		events <- tui.Event{Kind: "switch", Text: s.To}
		events <- tui.Event{Kind: "status", Text: statusFor(reg, s.To)}
	}
	ag.Emit = func(ev agent.Event) {
		switch ev.Type {
		case agent.EvToken:
			events <- tui.Event{Kind: "token", Text: ev.Text}
		case agent.EvToolStart:
			events <- tui.Event{Kind: "tool", Text: ev.Tool + " started"}
		case agent.EvToolEnd:
			events <- tui.Event{Kind: "tool", Text: ev.Tool + " done"}
			events <- tui.Event{Kind: "plan", Text: planCounts(sess)}
		case agent.EvAssistant:
			events <- tui.Event{Kind: "text", Text: ev.Text}
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
		events <- tui.Event{Kind: "text", Text: fmt.Sprintf("Zeuf ready — %d free models across %d backends. Type a task, /quit to exit.", len(router.FreeOnly(reg.Models())), len(reg.Backends()))}
		for {
			select {
			case <-ctx.Done():
				return
			case line, ok := <-submit:
				if !ok {
					return
				}
				if handleTUILine(ctx, strings.TrimSpace(line), &prefs, reg, r, ag, sess, events, p) {
					return
				}
			case act, ok := <-actions:
				if !ok {
					return
				}
				handleTUIAction(ctx, act, &prefs, reg, events)
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

// handleTUILine processes one submitted line. True means the UI should stop.
func handleTUILine(ctx context.Context, line string, prefs *router.Prefs, reg *router.Registry, r *router.Router, ag *agent.Agent, sess *agent.Session2, events chan tui.Event, p *tea.Program) bool {
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
		events <- tui.Event{Kind: "text", Text: "Commands: /models [all] · /connect · /router … · /providers · /session · /quit  (press ? anytime)"}
		return false
	case line == "/providers":
		events <- tui.Event{Kind: "text", Text: "Backends: " + strings.Join(reg.Backends(), ", ")}
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
	if _, err := ag.RunTurn(ctx, sess, *prefs); err != nil {
		events <- tui.Event{Kind: "error", Text: core.Redact(err.Error())}
	}
	return false
}

// handleTUIAction applies picker/wizard outcomes from the TUI.
func handleTUIAction(ctx context.Context, act tui.Action, prefs *router.Prefs, reg *router.Registry, events chan tui.Event) {
	switch a := act.(type) {
	case tui.ActionOpenPicker:
		events <- tui.Event{Kind: "picker", Models: pickerRows(reg, prefs, true)}
	case tui.ActionPin:
		prefs.PinnedModel = a.FullID
		events <- tui.Event{Kind: "status", Text: statusFor(reg, a.FullID)}
		events <- tui.Event{Kind: "text", Text: "Pinned model: " + a.FullID + "  (/router unpin for automatic)"}
	case tui.ActionUnpin:
		prefs.PinnedModel = ""
		events <- tui.Event{Kind: "status", Text: statusFor(reg, "")}
		events <- tui.Event{Kind: "text", Text: "Routing: automatic."}
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
	case tui.ActionLogin:
		refreshNow(ctx, reg)
		events <- tui.Event{Kind: "status", Text: statusFor(reg, "")}
		events <- tui.Event{Kind: "text", Text: fmt.Sprintf("Rescanned %s: %d free models available.", a.Backend, len(router.FreeOnly(reg.Models())))}
	}
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

// registerDirect adds a saved direct endpoint to the live registry.
func registerDirect(reg *router.Registry, name string) {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	for _, d := range cfg.Direct {
		if d.Name == name {
			reg.Register(direct.New(direct.Config{Name: d.Name, BaseURL: d.BaseURL, APIKeyEnv: d.APIKeyEnv}))
			return
		}
	}
}

// statusFor renders "model|provider|state" for the TUI status bar.
func statusFor(reg *router.Registry, override string) string {
	models := reg.Models()
	if len(models) == 0 {
		return "—|—|Offline"
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
	return short + "|" + m.Backend.Name() + "|" + state
}
