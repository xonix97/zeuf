package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"zeuf/internal/agent"
	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
	"zeuf/internal/mcp"
	"zeuf/internal/router"
	"zeuf/internal/skills"
)

// interactive runs the REPL coding session: the core milestone workflow.
func interactive(ctx context.Context, useTUI bool) error {
	if useTUI {
		return runTUI(ctx)
	}
	cfg, reg, r, tools, ag, err := session(ctx, "")
	if err != nil {
		return err
	}
	mgr := attachMCP(ctx, cfg, tools)
	defer mgr.Close()
	prefs := prefsFrom(cfg)
	fmt.Println("Zeuf — your coding agent. Type /help for commands, /quit to exit.")
	fmt.Fprintln(os.Stderr, "discovering models…")
	refreshNow(ctx, reg)
	fmt.Fprintf(os.Stderr, "%d models across %d backends.\n", len(reg.Models()), len(reg.Backends()))

	sess := agent.NewSession(newID(), "", tools)
	wireOutput(ag, r)
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1024*1024), 1024*1024)
	for {
		fmt.Print("\n> ")
		if !in.Scan() {
			fmt.Println()
			return nil
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			if handleSlash(line, &prefs, reg, sess, ag, tools, mgr) {
				return nil
			}
			continue
		}
		sess.Session.Task = line
		sess.AppendUser(line)
		out, err := ag.RunTurn(ctx, sess, prefs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nerror: %s\n", core.Redact(err.Error()))
		}
		if saveErr := saveSession(sess, tools.Workdir); saveErr != nil {
			fmt.Fprintf(os.Stderr, "\nwarning: save session: %v\n", core.Redact(saveErr.Error()))
		}
		_ = out
	}
}

func handleSlash(line string, prefs *router.Prefs, reg *router.Registry, sess *agent.Session2, ag *agent.Agent, treg *ct.Registry, mgr *mcp.Manager) (quit bool) {
	parts := strings.Fields(line)
	switch parts[0] {
	case "/quit", "/exit", "/q":
		return true
	case "/help":
		fmt.Println(`/router auto|balanced|fastest|quality — set routing strategy
/router pin <provider/id|id>       — pin a model
/router unpin                       — back to automatic
/router nofallback | /router fallback — toggle automatic fallback
/models [all]                        — pick the active model
/connect                             — attach a new model backend
/agents                              — list subagents this session
/sessions                            — list saved sessions
/resume <id>                         — resume a saved session
/rewind [n]                          — restore files to before recent turn(s)
/checkpoints                         — list rewind points
/skills                              — list skill playbooks
/skill <name>                        — load a skill into context
/mcp                                 — list MCP servers and tools
/providers                           — list backends
/session                             — show session summary
/quit                                — exit`)
	case "/router":
		handleRouter(parts[1:], prefs)
	case "/models":
		listed := reg.Models()
		if len(parts) < 2 || parts[1] != "all" {
			listed = router.FreeOnly(listed)
		}
		for i, e := range listed {
			mark := " "
			if prefs.PinnedModel == e.Model.FullID() || prefs.PinnedModel == e.Model.ID {
				mark = "●"
			}
			fmt.Printf("%s %2d) %s (%s, %s)\n", mark, i+1, e.Model.FullID(), e.Model.Availability, priceWord(e.Model))
		}
		fmt.Print("Pin number (enter keeps routing): ")
		var sel string
		fmt.Scanln(&sel)
		sel = strings.TrimSpace(sel)
		if sel == "" {
			break
		}
		var n int
		if _, err := fmt.Sscanf(sel, "%d", &n); err != nil || n < 1 || n > len(listed) {
			fmt.Println("invalid selection")
			break
		}
		prefs.PinnedModel = listed[n-1].Model.FullID()
		fmt.Println("pinned:", prefs.PinnedModel)
	case "/connect":
		if err := RunConnectREPL(context.Background(), bufio.NewReader(os.Stdin), func(s string) { fmt.Print(s) }); err != nil {
			if IsRescan(err) {
				refreshNow(context.Background(), reg)
				fmt.Printf("rescanned: %d free models.\n", len(router.FreeOnly(reg.Models())))
			} else {
				fmt.Println("connect failed:", err)
			}
			break
		}
		// Register any newly saved endpoint in this live session.
		syncDirect(reg)
		refreshNow(context.Background(), reg)
		fmt.Printf("connected: %d free models.\n", len(router.FreeOnly(reg.Models())))
	case "/providers":
		for _, n := range reg.Backends() {
			fmt.Println("-", n)
		}
	case "/agents":
		fmt.Println(formatAgents(ag.SnapshotSubs()))
	case "/sessions":
		list, err := core.ListSessions()
		if err != nil {
			fmt.Println("sessions failed:", err)
			break
		}
		if len(list) == 0 {
			fmt.Println("no saved sessions yet")
			break
		}
		for _, s := range list {
			fmt.Printf("%s  %s  %d turn(s)  %s\n", s.ID, s.Updated.Format("2006-01-02 15:04"), s.Turns, truncStr(s.Task, 60))
		}
	case "/resume":
		if len(parts) < 2 {
			fmt.Println("usage: /resume <id>  (see /sessions)")
			break
		}
		loaded, err := core.LoadSession(parts[1])
		if err != nil {
			fmt.Println("resume failed:", err)
			break
		}
		sess.Session = loaded
		if wd, ok := loaded.Meta["workdir"]; ok && wd != "" && wd != treg.Workdir {
			fmt.Printf("note: session was in %s, now in %s\n", wd, treg.Workdir)
		}
		fmt.Printf("resumed %s (%d message(s), %d checkpoint(s))\n", loaded.ID, len(loaded.Messages), len(loaded.Checkpoints))
	case "/rewind":
		n := 1
		if len(parts) > 1 {
			fmt.Sscanf(parts[1], "%d", &n)
		}
		out, err := agent.Rewind(sess, treg, n)
		if err != nil {
			fmt.Println("rewind failed:", err)
			break
		}
		for _, ln := range out {
			fmt.Println(ln)
		}
	case "/checkpoints":
		fmt.Println(formatCheckpoints(sess))
	case "/skills":
		fmt.Println(formatSkills(skills.Discover(treg.Workdir)))
	case "/skill":
		if len(parts) < 2 {
			fmt.Println("usage: /skill <name>  (see /skills)")
			break
		}
		s, ok := skills.Find(treg.Workdir, parts[1])
		if !ok {
			fmt.Printf("skill %q not found (see /skills)\n", parts[1])
			break
		}
		sess.Session.AppendSystem("Skill \"" + s.Name + "\" loaded:\n" + s.Body)
		fmt.Printf("skill %s loaded into context\n", s.Name)
	case "/mcp":
		fmt.Println(formatMCP(mgr))
	case "/session":
		fmt.Println(sess.Summary())
	default:
		fmt.Println("unknown command. Try /help.")
	}
	return false
}

func handleRouter(args []string, prefs *router.Prefs) {
	if len(args) == 0 {
		fmt.Printf("mode=%s pinned=%s fallback=%v\n", prefs.Mode, prefs.PinnedModel, prefs.FallbackEnabled)
		return
	}
	switch args[0] {
	case "auto", "balanced", "fastest", "quality":
		prefs.Mode = router.Mode(args[0])
		fmt.Println("routing mode:", prefs.Mode)
	case "pin":
		if len(args) < 2 {
			fmt.Println("usage: /router pin <provider/id>")
			return
		}
		prefs.PinnedModel = args[1]
		fmt.Println("pinned:", prefs.PinnedModel)
	case "unpin":
		prefs.PinnedModel = ""
		fmt.Println("unpinned: automatic routing")
	case "nofallback":
		prefs.FallbackEnabled = false
		fmt.Println("automatic fallback disabled")
	case "fallback":
		prefs.FallbackEnabled = true
		fmt.Println("automatic fallback enabled")
	default:
		fmt.Println("unknown /router option")
	}
}
