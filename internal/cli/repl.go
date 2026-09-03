package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"zeuf/internal/agent"
	"zeuf/internal/core"
	"zeuf/internal/router"
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
			if handleSlash(line, &prefs, reg, sess) {
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
		_ = out
	}
}

func handleSlash(line string, prefs *router.Prefs, reg *router.Registry, sess *agent.Session2) (quit bool) {
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
