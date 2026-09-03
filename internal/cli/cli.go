// Package cli implements the `zeuf` command line: interactive session,
// one-shot runs, model/provider inspection, config and doctor.
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zeuf/internal/agent"
	"zeuf/internal/config"
	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
	"zeuf/internal/providers/direct"
	"zeuf/internal/providers/kilo"
	"zeuf/internal/providers/opencode"
	"zeuf/internal/router"
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
		Use:   "zeuf",
		Short: "Zeuf — your own coding agent with many model sources",
		Long: `Zeuf is your own coding agent. It routes tasks across the model
backends you already have (OpenCode, Kilo Code, direct providers),
preserves the session across automatic fallbacks, and executes code
tasks with tools, approvals and streaming.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return interactive(cmd.Context(), tuiWanted())
		},
	}
	root.PersistentFlags().BoolVar(&flagAuto, "auto", false, "auto-approve non-destructive tool actions")
	root.PersistentFlags().StringVar(&flagDir, "dir", "", "working directory (default: current)")

	root.AddCommand(initCmd(), runCmd(), modelsCmd(), providersCmd(), configCmd(), doctorCmd(), tuiCmd(), connectCmd())
	return root
}

func tuiWanted() bool {
	for _, a := range os.Args[1:] {
		if a == "--tui" || a == "-t" {
			return true
		}
	}
	return false
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
	for _, d := range cfg.Direct {
		reg.Register(direct.New(direct.Config{Name: d.Name, BaseURL: d.BaseURL, APIKeyEnv: d.APIKeyEnv}))
	}
	r := router.New(reg)
	tools := ct.NewRegistry(workdir, ct.Policy{AutoApprove: cfg.AutoApprove, Approver: terminalApprover(cfg.AutoApprove)})
	ag := agent.New(r, tools)
	return cfg, reg, r, tools, ag, nil
}

func terminalApprover(auto bool) ct.Approver {
	return func(action, detail string) bool {
		if auto {
			return true
		}
		fmt.Printf("\nZeuf wants approval — %s\n  %s\nApprove? [y/N] ", action, core.Redact(detail))
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		return line == "y" || line == "yes"
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
		Use:   "run [task...]",
		Short: "Run a single task non-interactively",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			task := strings.Join(args, " ")
			cfg, reg, r, tools, ag, err := session(ctx, "")
			if err != nil {
				return err
			}
			prefs := prefsFrom(cfg)
			fmt.Fprintln(os.Stderr, "zeuf: discovering models…")
			refreshNow(ctx, reg)
			sess := agent.NewSession(newID(), task, tools)
			sess.AppendUser(task)
			wireOutput(ag, r)
			out, err := ag.RunTurn(ctx, sess, prefs)
			if err != nil {
				return err
			}
			fmt.Println(out)
			return nil
		},
	}
	c.Flags().StringVarP(&flagModel, "model", "m", "", "pin a model (provider/id or id)")
	c.Flags().StringVar(&flagMode, "mode", "", "routing mode: auto|balanced|fastest|quality")
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
			for _, bin := range []string{"opencode", "kilo"} {
				if p, err := lookPath(bin); err != nil {
					fmt.Printf("○ %-10s not found in PATH\n", bin)
				} else {
					fmt.Printf("● %-10s %s\n", bin, p)
				}
			}
			cfg, _ := config.Load()
			for _, d := range cfg.Direct {
				fmt.Printf("  direct %-10s %s key: %s\n", d.Name, d.BaseURL, KeyStatus(d))
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

// ---- output wiring -------------------------------------------------------------

func wireOutput(ag *agent.Agent, r *router.Router) {
	r.OnSwitch = func(s router.SwitchInfo) {
		fmt.Fprintf(os.Stderr, "\nModel limit reached. Continuing with %s.\n\n", s.To)
	}
	ag.Emit = func(ev agent.Event) {
		switch ev.Type {
		case agent.EvToken:
			fmt.Fprint(os.Stderr, ev.Text)
		case agent.EvToolStart:
			fmt.Fprintf(os.Stderr, "\n✓ %s…\n", ev.Tool)
		case agent.EvToolEnd:
			// tool results stream into context; keep the console quiet
		case agent.EvAssistant:
			if ev.Text != "" {
				fmt.Fprintf(os.Stderr, "\n%s\n", ev.Text)
			}
		case agent.EvSwitch:
			if ev.Switched != nil {
				fmt.Fprintf(os.Stderr, "\nModel limit reached. Continuing with %s.\n", ev.Switched.To)
			}
		}
	}
}

func newID() string { return fmt.Sprintf("zeuf-%d", time.Now().UnixNano()) }
