// Package cli implements the `zeuf` command line: interactive session,
// one-shot runs, model/provider inspection, config and doctor.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"zeuf/internal/agent"
	"zeuf/internal/config"
	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
	"zeuf/internal/providers/anthropic"
	"zeuf/internal/providers/antigravity"
	"zeuf/internal/providers/direct"
	"zeuf/internal/providers/gemini"
	"zeuf/internal/providers/kilo"
	"zeuf/internal/providers/opencode"
	"zeuf/internal/router"
	"zeuf/internal/tui"
)

var (
	flagAuto  bool
	flagModel string
	flagMode  string
	flagDir   string
)

// Execute runs the root command.
func Execute() error { return rootCmd().Execute() }

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "zeuf",
		Version: tui.Version,
		Short:   "Zeuf — your own coding agent with many model sources",
		Long: `Zeuf is your own coding agent. It routes tasks across the model
backends you already have (OpenCode, Kilo Code, direct providers),
preserves the session across automatic fallbacks, and executes code
tasks with tools, approvals and streaming.

Run bare 'zeuf' in a terminal for the full-screen TUI;
use --plain for line-based output, or a subcommand for one-shot use.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			forceTUI, _ := cmd.Flags().GetBool("tui")
			forcePlain, _ := cmd.Flags().GetBool("plain")
			return interactive(cmd.Context(), forceTUI || (!forcePlain && hasTerminal()))
		},
	}
	root.PersistentFlags().BoolVar(&flagAuto, "auto", false, "auto-approve non-destructive tool actions")
	root.PersistentFlags().StringVar(&flagDir, "dir", "", "working directory (default: current)")
	root.PersistentFlags().Bool("tui", false, "force the full-screen TUI (default when interactive)")
	root.PersistentFlags().Bool("plain", false, "force plain CLI output instead of the TUI")

	root.AddCommand(initCmd(), runCmd(), modelsCmd(), providersCmd(), configCmd(), doctorCmd(), tuiCmd(), connectCmd(), sessionsCmd(), mcpCmd(), agentsCmd(), planCmd(), statusCmd())
	return root
}

// hasTerminal reports whether we're attached to a real terminal that can
// host the full-screen TUI. Piped/scripted use falls back to plain output.
func hasTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// session bootstraps config, registry, router, tools and agent.
func session(ctx context.Context, workdir string) (config.Config, *router.Registry, *router.Router, *ct.Registry, *agent.Agent, error) {
	cfg, err := config.Load()
	if err != nil {
		return cfg, nil, nil, nil, nil, err
	}
	if flagAuto {
		cfg.AutoApprove = true
	}
	if workdir == "" {
		workdir = flagDir
	}
	if workdir == "" {
		workdir = cfg.Workdir
	}
	if workdir == "" {
		workdir, _ = os.Getwd()
	}
	reg := router.NewRegistry()
	reg.Register(opencode.New(opencode.Config{ServeURL: cfg.OpencodeServe, Workdir: workdir}))
	reg.Register(kilo.New(kilo.Config{ServeURL: cfg.KiloServe, Workdir: workdir}))
	reg.Register(gemini.New(gemini.Config{Workdir: workdir}))
	reg.Register(antigravity.New(antigravity.Config{Workdir: workdir}))
	for _, d := range cfg.Direct {
		if d.Type == "anthropic" || d.Name == "anthropic" || strings.Contains(d.BaseURL, "anthropic.com") {
			reg.Register(anthropic.New(anthropic.Config{Name: d.Name, BaseURL: d.BaseURL, APIKeyEnv: d.APIKeyEnv}))
		} else {
			reg.Register(direct.New(direct.Config{Name: d.Name, BaseURL: d.BaseURL, APIKeyEnv: d.APIKeyEnv}))
		}
	}
	autoRegisterEnvBackends(reg, cfg)
	r := router.New(reg)
	tools := ct.NewRegistry(workdir, ct.Policy{AutoApprove: cfg.AutoApprove, Approver: terminalApprover(cfg.AutoApprove)})
	ag := agent.New(r, tools)
	return cfg, reg, r, tools, ag, nil
}

func autoRegisterEnvBackends(reg *router.Registry, cfg config.Config) {
	configured := map[string]bool{}
	for _, d := range cfg.Direct {
		configured[d.Name] = true
	}

	type envBackend struct {
		name        string
		baseURL     string
		envKey      string
		isAnthropic bool
	}
	candidates := []envBackend{
		{name: "anthropic", baseURL: "https://api.anthropic.com/v1", envKey: "ANTHROPIC_API_KEY", isAnthropic: true},
		{name: "deepseek", baseURL: "https://api.deepseek.com", envKey: "DEEPSEEK_API_KEY"},
		{name: "groq", baseURL: "https://api.groq.com/openai/v1", envKey: "GROQ_API_KEY"},
		{name: "openai", baseURL: "https://api.openai.com/v1", envKey: "OPENAI_API_KEY"},
		{name: "mistral", baseURL: "https://api.mistral.ai/v1", envKey: "MISTRAL_API_KEY"},
		{name: "codestral", baseURL: "https://codestral.mistral.ai/v1", envKey: "CODESTRAL_API_KEY"},
		{name: "openrouter", baseURL: "https://openrouter.ai/api/v1", envKey: "OPENROUTER_API_KEY"},
		{name: "together", baseURL: "https://api.together.xyz/v1", envKey: "TOGETHER_API_KEY"},
		{name: "fireworks", baseURL: "https://api.fireworks.ai/inference/v1", envKey: "FIREWORKS_API_KEY"},
		{name: "xai", baseURL: "https://api.x.ai/v1", envKey: "XAI_API_KEY"},
	}

	for _, c := range candidates {
		if configured[c.name] {
			continue
		}
		if val := strings.TrimSpace(os.Getenv(c.envKey)); val != "" {
			if c.isAnthropic {
				reg.Register(anthropic.New(anthropic.Config{Name: c.name, BaseURL: c.baseURL, APIKeyEnv: c.envKey}))
			} else {
				reg.Register(direct.New(direct.Config{Name: c.name, BaseURL: c.baseURL, APIKeyEnv: c.envKey}))
			}
		}
	}
}

func terminalApprover(auto bool) ct.Approver {
	always := map[string]bool{}
	return func(action, detail string) bool {
		if auto || always[action] {
			return true
		}
		fmt.Printf("\nZeuf wants approval — %s\n  %s\nApprove? [y]es / [a]lways this session / [N]o ", action, core.Redact(detail))
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		switch line {
		case "a", "always":
			always[action] = true
			fmt.Printf("Always allowing this session: %s\n", action)
			return true
		default:
			return line == "y" || line == "yes"
		}
	}
}

func refreshNow(ctx context.Context, reg *router.Registry) {
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	reg.Refresh(cctx)
}

func prefsFrom(cfg config.Config) router.Prefs {
	p := cfg.Prefs
	if flagModel != "" {
		p.PinnedModel = flagModel
	}
	if flagMode != "" {
		p.Mode = router.Mode(flagMode)
	}
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 4
	}
	return p
}

// ---- run -------------------------------------------------------------------

func runCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "run [task...]",
		Aliases: []string{"exec"},
		Short:   "Run a single task non-interactively",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			task := strings.Join(args, " ")
			cfg, reg, r, tools, ag, err := session(ctx, "")
			if err != nil {
				return err
			}
			mgr := attachMCP(ctx, cfg, tools)
			defer mgr.Close()
			prefs := prefsFrom(cfg)
			jsonOut, _ := cmd.Flags().GetBool("json")
			if !jsonOut {
				fmt.Fprintln(os.Stderr, "zeuf: discovering models…")
			}
			refreshNow(ctx, reg)
			var sess *agent.Session2
			if resumeID, _ := cmd.Flags().GetString("resume"); resumeID != "" {
				loaded, err := core.LoadSession(resumeID)
				if err != nil {
					return err
				}
				sess = agent.NewSession(loaded.ID, task, tools)
				sess.Session = loaded
				if !jsonOut {
					fmt.Fprintf(os.Stderr, "zeuf: resumed session %s (%d message(s))\n", loaded.ID, len(loaded.Messages))
				}
			} else {
				sess = agent.NewSession(newID(), task, tools)
			}
			sess.AppendUser(task)
			var state func() (bool, bool)
			if !jsonOut {
				state = wireOutput(ag, r)
			}
			out, err := ag.RunTurn(ctx, sess, prefs)
			if saveErr := saveSession(sess, tools.Workdir); saveErr != nil && !jsonOut {
				fmt.Fprintf(os.Stderr, "zeuf: save session: %v\n", core.Redact(saveErr.Error()))
			}
			if err != nil {
				return err
			}
			if jsonOut {
				lastModel := ""
				if len(sess.Session.SwitchTrail) > 0 {
					lastModel = sess.Session.SwitchTrail[len(sess.Session.SwitchTrail)-1]
				}
				res := map[string]any{
					"task":       task,
					"output":     out,
					"model":      lastModel,
					"turns":      len(sess.Session.Messages),
					"tokens_in":  sess.Session.TokensIn,
					"tokens_out": sess.Session.TokensOut,
				}
				data, _ := json.MarshalIndent(res, "", "  ")
				fmt.Println(string(data))
				return nil
			}
			// The final text already went to a TTY stderr via the stream or
			// the assistant echo; print it only when the user hasn't seen it
			// (piped use stays scriptable).
			var streamed, echoed bool
			if state != nil {
				streamed, echoed = state()
			}
			if !term.IsTerminal(int(os.Stdout.Fd())) || (!streamed && !echoed) {
				fmt.Println(out)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&flagModel, "model", "m", "", "pin a model (provider/id or id)")
	c.Flags().StringVar(&flagMode, "mode", "", "routing mode: auto|balanced|fastest|quality")
	c.Flags().String("resume", "", "resume a saved session id (see `zeuf sessions`)")
	c.Flags().Bool("json", false, "output result as structured JSON for automation pipelines")
	return c
}

// ---- models ------------------------------------------------------------------

func modelsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "models",
		Short: "List free models Zeuf currently has access to",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			showAll, _ := cmd.Flags().GetBool("all")
			_, reg, _, _, _, err := session(ctx, "")
			if err != nil {
				return err
			}
			refreshNow(ctx, reg)
			all := reg.Models()
			listed := all
			if showAll {
				fmt.Printf("AVAILABLE MODELS (%d)\n", len(listed))
			} else {
				listed = router.FreeOnly(all)
				fmt.Printf("FREE MODELS (%d of %d — paid/unknown-cost hidden, use --all to include them)\n", len(listed), len(all))
			}
			for _, e := range listed {
				status := statusGlyph(e.Model.Availability) + " " + string(e.Model.Availability)
				ctxLen := "Unknown"
				if e.Model.Caps.ContextLength > 0 {
					ctxLen = fmt.Sprintf("%dK", e.Model.Caps.ContextLength/1000)
				}
				fmt.Printf("%s %s\n  %s\n  Coding: %s  Context: %s  Tools: %s  Price: %s\n  Status: %s",
					statusGlyph(e.Model.Availability), e.Model.FullID(),
					e.Model.DisplayName,
					scoreWord(e.Model.Scores.Coding), ctxLen, yesNo(e.Model.Caps.SupportsTools), priceWord(e.Model), status)
				if e.Model.QuotaState != "" && e.Model.QuotaState != "unknown" {
					fmt.Printf("  Quota: %s", e.Model.QuotaState)
				} else {
					fmt.Printf("  Quota: Unknown")
				}
				if e.Model.LastError != "" {
					fmt.Printf("\n  Last error: %s", core.Redact(e.Model.LastError))
				}
				fmt.Println()
			}
			return nil
		},
	}
	c.Flags().Bool("all", false, "include paid and unknown-cost models")
	return c
}

func priceWord(m core.ModelInfo) string {
	if m.IsFree {
		return "Free"
	}
	if m.CostKnown {
		return "Paid"
	}
	return "Unknown"
}
func statusGlyph(a core.Availability) string {
	if a == core.AvailAvailable {
		return "●"
	}
	return "○"
}

func scoreWord(v float64) string {
	switch {
	case v < 0:
		return "Unknown"
	case v >= 0.85:
		return "Excellent"
	case v >= 0.65:
		return "Good"
	case v >= 0.4:
		return "Fair"
	default:
		return "Limited"
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// ---- providers ---------------------------------------------------------------

func providersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "providers",
		Short: "Show backend health",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			_, reg, _, _, _, err := session(ctx, "")
			if err != nil {
				return err
			}
			// Health per backend is derived from discovery: reachable
			// backends list models without spending quota.
			refreshNow(ctx, reg)
			counts := map[string]int{}
			for _, e := range reg.Models() {
				counts[e.Backend.Name()]++
			}
			for _, name := range reg.Backends() {
				fmt.Printf("● %-14s models: %d\n", name, counts[name])
			}
			return nil
		},
	}
}

// ---- config ------------------------------------------------------------------

func configCmd() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "View or edit configuration"}
	c.AddCommand(
		&cobra.Command{Use: "path", Short: "Print config path", Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(config.Path())
		}},
		&cobra.Command{Use: "show", Short: "Print current config (secrets never stored here)", RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			fmt.Printf("path: %s\nbackends: %s\nmode: %s\nfallback: %v\npinned: %s\nauto_approve: %v\ndirect endpoints: %d\n",
				config.Path(), strings.Join(cfg.BackendsOrder, ","), cfg.Prefs.Mode, cfg.Prefs.FallbackEnabled, cfg.Prefs.PinnedModel, cfg.AutoApprove, len(cfg.Direct))
			for _, d := range cfg.Direct {
				set := "missing"
				if os.Getenv(d.APIKeyEnv) != "" {
					set = "set"
				}
				fmt.Printf("  - %s %s (%s: %s)\n", d.Name, d.BaseURL, d.APIKeyEnv, set)
			}
			return nil
		}},
		&cobra.Command{Use: "set <key> <value>", Short: "Set a key (prefs.mode, prefs.pinned, fallback, auto_approve)", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			switch args[0] {
			case "prefs.mode":
				cfg.Prefs.Mode = router.Mode(args[1])
			case "prefs.pinned":
				cfg.Prefs.PinnedModel = args[1]
			case "fallback":
				cfg.Prefs.FallbackEnabled = args[1] == "true" || args[1] == "on" || args[1] == "1"
			case "auto_approve":
				cfg.AutoApprove = args[1] == "true" || args[1] == "on" || args[1] == "1"
			default:
				return fmt.Errorf("unknown key %q", args[0])
			}
			return config.Save(cfg)
		}},
	)
	return c
}

// ---- init --------------------------------------------------------------------

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize Zeuf configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			p := config.Path()
			if _, err := os.Stat(p); err == nil {
				fmt.Println("already initialized:", p)
				return nil
			}
			cfg := config.Default()
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Println("wrote", p)
			fmt.Println("Next: `zeuf doctor`, then `zeuf` to start. Add direct providers by editing the config file (API keys stay in env vars).")
			return nil
		},
	}
}

// ---- doctor ------------------------------------------------------------------

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check binaries, config and backend reachability (never prints secrets)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			fmt.Println("Zeuf doctor")
			fmt.Println("config:", config.Path())
			for _, bin := range []string{"opencode", "kilo", "gemini"} {
				if p, err := lookPath(bin); err != nil {
					fmt.Printf("○ %-10s not found in PATH (install.sh installs it)\n", bin)
				} else {
					fmt.Printf("● %-10s %s\n", bin, p)
				}
			}
			cfg, _ := config.Load()
			for _, d := range cfg.Direct {
				fmt.Printf("  direct %-10s %s key: %s\n", d.Name, d.BaseURL, KeyStatus(d))
			}
			for name := range cfg.MCPServers {
				fmt.Printf("  mcp %-14s (see /mcp when running)\n", name)
			}
			_, reg, _, _, _, err := session(ctx, "")
			if err != nil {
				return err
			}
			refreshNow(ctx, reg)
			fmt.Printf("\nmodels discovered: %d\n", len(reg.Models()))
			return nil
		},
	}
}

func lookPath(bin string) (string, error) {
	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		p := dir + "/" + bin
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("not found")
}

// ---- agents -----------------------------------------------------------------

func agentsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "agents",
		Short: "Inspect specialist subagents from current or latest session",
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := core.ListSessions()
			if err != nil || len(list) == 0 {
				fmt.Println("no session records found")
				return nil
			}
			sessID, _ := cmd.Flags().GetString("session")
			if sessID == "" {
				sessID = list[0].ID
			}
			loaded, err := core.LoadSession(sessID)
			if err != nil {
				return err
			}
			fmt.Printf("Session: %s (Task: %s)\n", loaded.ID, loaded.Task)
			if len(loaded.TaskGraphData) > 0 {
				if g, err := agent.FromJSON(loaded.TaskGraphData); err == nil {
					fmt.Println("\nSpecialist Tasks:")
					for _, t := range g.TasksList() {
						mark := "○"
						switch t.Status {
						case agent.TaskCompleted:
							mark = "✓"
						case agent.TaskRunning:
							mark = "●"
						case agent.TaskFailed:
							mark = "✗"
						case agent.TaskBlocked:
							mark = "⊘"
						}
						fmt.Printf("%s [%s] %-12s %s (status: %s)\n", mark, t.ID, t.AssignedAgent, t.Title, t.Status)
						if t.Result != "" {
							fmt.Printf("    Result: %s\n", truncStr(t.Result, 80))
						}
					}
					return nil
				}
			}
			fmt.Println("No subagent task graph recorded for this session.")
			return nil
		},
	}
	c.Flags().String("session", "", "session id (default: latest)")
	return c
}

// ---- plan -------------------------------------------------------------------

func planCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "plan",
		Short: "Inspect the task plan from current or latest session",
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := core.ListSessions()
			if err != nil || len(list) == 0 {
				fmt.Println("no session records found")
				return nil
			}
			sessID, _ := cmd.Flags().GetString("session")
			if sessID == "" {
				sessID = list[0].ID
			}
			loaded, err := core.LoadSession(sessID)
			if err != nil {
				return err
			}
			fmt.Printf("Session: %s\nTask: %s\n\n", loaded.ID, loaded.Task)
			if len(loaded.TaskGraphData) > 0 {
				if g, err := agent.FromJSON(loaded.TaskGraphData); err == nil {
					fmt.Println(g.Format())
					return nil
				}
			}
			if len(loaded.Plan) > 0 {
				for i, p := range loaded.Plan {
					mark := "[ ]"
					if p.Done {
						mark = "[x]"
					}
					fmt.Printf("%d. %s %s\n", i+1, mark, p.Title)
					if p.Detail != "" {
						fmt.Printf("   %s\n", p.Detail)
					}
				}
				return nil
			}
			fmt.Println("No plan recorded for this session.")
			return nil
		},
	}
	c.Flags().String("session", "", "session id (default: latest)")
	return c
}

// ---- status -----------------------------------------------------------------

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Display repository, git, router, and backend status",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, reg, _, tools, _, err := session(ctx, "")
			if err != nil {
				return err
			}
			prefs := prefsFrom(cfg)

			fmt.Println("ZEUF STATUS")
			fmt.Printf("Workdir: %s\n", tools.Workdir)
			branch, dirty := tools.GitInfo()
			if branch != "" {
				dirtyNote := "clean"
				if dirty != "" {
					dirtyNote = "dirty (*)"
				}
				fmt.Printf("Git: branch %s (%s)\n", branch, dirtyNote)
			} else {
				fmt.Println("Git: not a git repository")
			}

			// Discovered build systems
			ev, _ := agent.Discover(ctx, tools.Workdir, "", tools)
			if ev != nil && ev.BuildSystem != "" {
				fmt.Printf("Build System: %s (default test: %s)\n", ev.BuildSystem, ev.TestCommand)
			}

			// Router info
			fmt.Printf("\nRouter:\n")
			fmt.Printf("  Mode: %s\n", prefs.Mode)
			if prefs.PinnedModel != "" {
				fmt.Printf("  Pinned Model: %s\n", prefs.PinnedModel)
			} else {
				fmt.Printf("  Pinned Model: (auto routing)\n")
			}
			fmt.Printf("  Fallback: %v (max attempts: %d)\n", prefs.FallbackEnabled, prefs.MaxAttempts)

			// Models count
			refreshNow(ctx, reg)
			freeModels := router.FreeOnly(reg.Models())
			fmt.Printf("  Available Models: %d total (%d free) across %d backends\n", len(reg.Models()), len(freeModels), len(reg.Backends()))
			return nil
		},
	}
}

// ---- output wiring -------------------------------------------------------------

func wireOutput(ag *agent.Agent, r *router.Router) func() (streamed, echoed bool) {
	r.OnSwitch = func(s router.SwitchInfo) {
		fmt.Fprintf(os.Stderr, "\n[MODEL SWITCH] %s -> %s (reason: %s)\n\n", s.From, s.To, s.Reason)
	}
	var streamed, echoed bool
	ag.Emit = func(ev agent.Event) {
		switch ev.Type {
		case agent.EvPhase:
			fmt.Fprintf(os.Stderr, "\n[%s] %s\n", ev.Phase, ev.Text)
		case agent.EvGraph:
			if ev.Graph != nil {
				fmt.Fprintf(os.Stderr, "\n%s\n", ev.Graph.Format())
			}
		case agent.EvSubStart:
			fmt.Fprintf(os.Stderr, "  ● [Agent: %s] Starting [%s]: %s\n", ev.Role, ev.TaskID, ev.Text)
		case agent.EvSubEnd:
			status := "✓ completed"
			if !ev.Ok {
				status = "✗ failed"
			}
			fmt.Fprintf(os.Stderr, "  %s [Agent: %s] Task [%s] (%s)\n", status, ev.Role, ev.TaskID, ev.Duration.Round(time.Millisecond))
		case agent.EvVerifyStart:
			fmt.Fprintf(os.Stderr, "  ● [Verify] Running: %s\n", ev.Text)
		case agent.EvVerifyEnd:
			status := "✓ PASSED"
			if !ev.Ok {
				status = "✗ FAILED"
			}
			fmt.Fprintf(os.Stderr, "  %s [Verify] %s (%s)\n", status, ev.Text, ev.Duration.Round(time.Millisecond))
			if !ev.Ok && ev.Diagnosis != "" {
				fmt.Fprintf(os.Stderr, "    diagnosis: %s\n", ev.Diagnosis)
			}
		case agent.EvDiff:
			if ev.DiffStat != "" {
				fmt.Fprintf(os.Stderr, "\n[Changes]\n%s\n", ev.DiffStat)
			}
		case agent.EvReasoning:
			fmt.Fprintf(os.Stderr, "\033[90m%s\033[0m", ev.Text)
		case agent.EvToken:
			streamed = true
			fmt.Fprint(os.Stderr, ev.Text)
		case agent.EvToolStart:
			streamed = false
			fmt.Fprintf(os.Stderr, "\n  ↳ %s %s\n", ev.Tool, ev.Text)
		case agent.EvToolEnd:
			// keep tool end compact
		case agent.EvAssistant:
			if ev.Text != "" && !streamed {
				echoed = true
				fmt.Fprintf(os.Stderr, "\n%s\n", ev.Text)
			}
		case agent.EvSwitch:
			if ev.Switched != nil {
				fmt.Fprintf(os.Stderr, "\n[MODEL SWITCH] %s -> %s\n", ev.Switched.From, ev.Switched.To)
			}
		}
	}
	return func() (bool, bool) { return streamed, echoed }
}

func newID() string { return fmt.Sprintf("zeuf-%d", time.Now().UnixNano()) }
