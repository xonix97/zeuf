package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"zeuf/internal/agent"
	"zeuf/internal/config"
	"zeuf/internal/core"
	"zeuf/internal/core/tools"
	"zeuf/internal/mcp"
	"zeuf/internal/skills"
)

// attachMCP starts configured MCP servers and registers their tools.
// Absent configuration is a no-op; each server failure is isolated.
func attachMCP(ctx context.Context, cfg config.Config, tools *tools.Registry) *mcp.Manager {
	if len(cfg.MCPServers) == 0 {
		return mcp.NewManager()
	}
	mgr := mcp.NewManager()
	mcfg := make(map[string]mcp.ServerConfig, len(cfg.MCPServers))
	for n, s := range cfg.MCPServers {
		mcfg[n] = mcp.ServerConfig{Command: s.Command, Args: s.Args, Env: s.Env}
	}
	mgr.Start(ctx, mcfg)
	if n := mgr.AsTools(tools); n > 0 {
		fmt.Fprintf(os.Stderr, "zeuf: %d MCP tool(s) attached\n", n)
	}
	for _, st := range mgr.Status() {
		if !st.OK {
			fmt.Fprintf(os.Stderr, "zeuf: mcp %s unavailable: %s\n", st.Name, core.Redact(st.Err))
		}
	}
	return mgr
}

// saveSession persists with the workdir recorded; errors go to stderr
// (CLI paths) — callers with an event channel report there instead.
func saveSession(sess *agent.Session2, workdir string) error {
	if sess.Session.Meta == nil {
		sess.Session.Meta = map[string]string{}
	}
	sess.Session.Meta["workdir"] = workdir
	return core.SaveSession(sess.Session)
}

func sessionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "List saved sessions (resume with `zeuf run --resume ID` or /resume)",
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := core.ListSessions()
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Println("no saved sessions yet")
				return nil
			}
			for _, s := range list {
				task := s.Task
				if task == "" {
					task = "(no task)"
				}
				fmt.Printf("%s  %s  %d turn(s)  %s\n", s.ID, s.Updated.Format("2006-01-02 15:04"), s.Turns, truncStr(task, 60))
			}
			return nil
		},
	}
}

func truncStr(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "List configured MCP servers and their tools",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if len(cfg.MCPServers) == 0 {
				fmt.Println("no MCP servers configured (see config mcp_servers)")
				return nil
			}
			mgr := attachMCP(ctx, cfg, dummyRegistry())
			defer mgr.Close()
			for _, st := range mgr.Status() {
				if st.OK {
					fmt.Printf("● %-14s %d tool(s)\n", st.Name, st.Tools)
				} else {
					fmt.Printf("○ %-14s %s\n", st.Name, core.Redact(st.Err))
				}
			}
			return nil
		},
	}
}

func dummyRegistry() *tools.Registry {
	dir, _ := os.Getwd()
	return tools.NewRegistry(dir, tools.Policy{})
}

// formatCheckpoints lists rewind points newest-last with file counts.
func formatCheckpoints(sess *agent.Session2) string {
	cps := sess.Session.Checkpoints
	if len(cps) == 0 {
		return "No checkpoints yet — files Zeuf writes are snapshotted per turn."
	}
	var b strings.Builder
	for i, cp := range cps {
		label := cp.Label
		if label == "" {
			label = "(task)"
		}
		fmt.Fprintf(&b, "%d) %s — %d file(s) [%s]\n", len(cps)-i, truncStr(label, 50), len(cp.Files), cp.At.Format("15:04:05"))
	}
	b.WriteString("rewind with /rewind [n]")
	return strings.TrimRight(b.String(), "\n")
}

// formatSkills lists discovered skills.
func formatSkills(found []skills.Skill) string {
	if len(found) == 0 {
		return "No skills found (add SKILL.md playbooks under ~/.config/zeuf/skills/<name>/ or .zeuf/skills/<name>/)."
	}
	var b strings.Builder
	for _, s := range found {
		desc := s.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "- %s [%s] — %s\n", s.Name, s.Source, desc)
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatMCP renders manager status for /mcp.
func formatMCP(mgr *mcp.Manager) string {
	st := mgr.Status()
	if len(st) == 0 {
		return "No MCP servers configured (see config mcp_servers)."
	}
	var b strings.Builder
	for _, s := range st {
		if s.OK {
			fmt.Fprintf(&b, "● %s — %d tool(s)\n", s.Name, s.Tools)
		} else {
			fmt.Fprintf(&b, "○ %s — %s\n", s.Name, core.Redact(s.Err))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
