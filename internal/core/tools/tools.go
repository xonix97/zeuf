// Package tools implements Zeuf's tool runtime: repository inspection,
// file operations, terminal, git, search and planning. Tools return
// structured, size-bounded results. Potentially dangerous operations go
// through the approval policy and are never executed silently.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxOutputBytes = 32 * 1024

// Approver decides whether a sensitive action may proceed.
// The UI injects an interactive implementation; headless mode uses policy.
type Approver func(action, detail string) bool

// Policy controls which operations need explicit approval.
type Policy struct {
	// AutoApprove allows non-destructive writes/edits/safe commands without prompting.
	AutoApprove bool
	Approver    Approver
}

// Result is a structured, bounded tool outcome.
type Result struct {
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
	Truncated bool   `json:"truncated"`
}

func ok(content string) (Result, error) {
	if len(content) > maxOutputBytes {
		return Result{Content: content[:maxOutputBytes] + "\n…[truncated]", Truncated: true}, nil
	}
	return Result{Content: content}, nil
}

func fail(format string, args ...any) (Result, error) {
	return Result{Content: fmt.Sprintf(format, args...), IsError: true}, nil
}

// Tool is a single executable capability.
type Tool struct {
	Name        string
	Description string
	Parameters  string // JSON Schema
	Run         func(ctx context.Context, argsJSON string) (Result, error)
}

// Registry holds the tool set for one working directory.
type Registry struct {
	tools   map[string]Tool
	Workdir string
	Policy  Policy
	plan    *PlanStore
}

// PlanStore is the backing state for the `plan` tool.
type PlanStore struct {
	mu    sync.Mutex
	Steps []PlanStep
}

// PlanStep mirrors the session plan.
type PlanStep struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Done   bool   `json:"done"`
}

// NewRegistry builds the default tool set rooted at workdir.
func NewRegistry(workdir string, p Policy) *Registry {
	if workdir == "" {
		workdir, _ = os.Getwd()
	}
	r := &Registry{tools: map[string]Tool{}, Workdir: workdir, Policy: p, plan: &PlanStore{}}
	r.registerDefaults()
	return r
}

// Names returns sorted tool names.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.tools))
	for n := range r.tools {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) { t, ok := rangeMap(r.tools, name); return t, ok }

// AddTool registers an additional tool (e.g. the orchestrator's delegate).
func (r *Registry) AddTool(t Tool) { r.tools[t.Name] = t }

func rangeMap(m map[string]Tool, k string) (Tool, bool) { t, ok := m[k]; return t, ok }

// ToolDefs returns definitions in the shared shape used by providers.
func (r *Registry) ToolDefs() []ToolDefLike {
	names := r.Names()
	out := make([]ToolDefLike, 0, len(names))
	for _, n := range names {
		t := r.tools[n]
		out = append(out, ToolDefLike{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	return out
}

// ToolDefLike mirrors core.ToolDef without importing core (tools is a leaf).
type ToolDefLike struct {
	Name        string
	Description string
	Parameters  string
}

// Execute runs a named tool with a JSON argument object.
func (r *Registry) Execute(ctx context.Context, name, argsJSON string) (Result, error) {
	t, ok := r.tools[name]
	if !ok {
		return fail("unknown tool %q (available: %s)", name, strings.Join(r.Names(), ", "))
	}
	return t.Run(ctx, argsJSON)
}

// approve consults policy for a sensitive action.
func (r *Registry) approve(action, detail string) bool {
	if r.Policy.Approver != nil {
		return r.Policy.Approver(action, detail)
	}
	return r.Policy.AutoApprove
}

func (r *Registry) resolve(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(r.Workdir, path))
}

// outsideWorkdir reports whether path escapes the workdir.
func (r *Registry) outsideWorkdir(path string) bool {
	abs := r.resolve(path)
	rel, err := filepath.Rel(r.Workdir, abs)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

var destructiveCmd = regexp.MustCompile(`(?i)(rm\s+-rf?\s+/(?:\s|$)|mkfs|dd\s+[^|]*of=\s*/dev/|:\(\)\s*\{|shutdown|halt|reboot|git\s+clean\s+-fdx?\b)`)

func (r *Registry) registerDefaults() {
	r.tools["read"] = Tool{
		Name:        "read",
		Description: "Read a file (optionally a line range).",
		Parameters:  `{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer"},"limit":{"type":"integer"}},"required":["path"]}`,
		Run: func(ctx context.Context, argsJSON string) (Result, error) {
			var a struct {
				Path   string `json:"path"`
				Offset int    `json:"offset"`
				Limit  int    `json:"limit"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
				return fail("bad arguments: %v", err)
			}
			data, err := os.ReadFile(r.resolve(a.Path))
			if err != nil {
				return fail("read %s: %v", a.Path, err)
			}
			lines := strings.Split(string(data), "\n")
			start := a.Offset
			if start < 0 {
				start = 0
			}
			if start > len(lines) {
				start = len(lines)
			}
			end := len(lines)
			if a.Limit > 0 && start+a.Limit < end {
				end = start + a.Limit
			}
			var b strings.Builder
			for i := start; i < end; i++ {
				fmt.Fprintf(&b, "%d: %s\n", i+1, lines[i])
			}
			return ok(b.String())
		},
	}
	r.tools["write"] = Tool{
		Name:        "write",
		Description: "Create or overwrite a file. Requires approval unless auto-approved.",
		Parameters:  `{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`,
		Run: func(ctx context.Context, argsJSON string) (Result, error) {
			var a struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
				return fail("bad arguments: %v", err)
			}
			abs := r.resolve(a.Path)
			if r.outsideWorkdir(a.Path) || !r.Policy.AutoApprove {
				if !r.approve("write file", abs) {
					return fail("write %s denied by approval policy", abs)
				}
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return fail("mkdir: %v", err)
			}
			if err := os.WriteFile(abs, []byte(a.Content), 0o644); err != nil {
				return fail("write %s: %v", a.Path, err)
			}
			return ok(fmt.Sprintf("wrote %d bytes to %s", len(a.Content), abs))
		},
	}
	r.tools["edit"] = Tool{
		Name:        "edit",
		Description: "Replace the first occurrence of old_string with new_string in a file. Requires approval unless auto-approved.",
		Parameters:  `{"type":"object","properties":{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"}},"required":["path","old_string","new_string"]}`,
		Run: func(ctx context.Context, argsJSON string) (Result, error) {
			var a struct {
				Path string `json:"path"`
				Old  string `json:"old_string"`
				New  string `json:"new_string"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
				return fail("bad arguments: %v", err)
			}
			abs := r.resolve(a.Path)
			if !r.Policy.AutoApprove {
				if !r.approve("edit file", abs) {
					return fail("edit %s denied by approval policy", abs)
				}
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				return fail("read %s: %v", a.Path, err)
			}
			if !strings.Contains(string(data), a.Old) {
				return fail("old_string not found in %s", abs)
			}
			updated := strings.Replace(string(data), a.Old, a.New, 1)
			if err := os.WriteFile(abs, []byte(updated), 0o644); err != nil {
				return fail("write %s: %v", a.Path, err)
			}
			return ok(fmt.Sprintf("edited %s", abs))
		},
	}
	r.tools["bash"] = Tool{
		Name:        "bash",
		Description: "Run a shell command in the workdir. Destructive commands always need approval.",
		Parameters:  `{"type":"object","properties":{"command":{"type":"string"},"timeout_ms":{"type":"integer"}},"required":["command"]}`,
		Run: func(ctx context.Context, argsJSON string) (Result, error) {
			var a struct {
				Command   string `json:"command"`
				TimeoutMs int    `json:"timeout_ms"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
				return fail("bad arguments: %v", err)
			}
			if a.Command == "" {
				return fail("empty command")
			}
			if destructiveCmd.MatchString(a.Command) {
				if !r.approve("run destructive command", a.Command) {
					return fail("command denied by approval policy: %s", a.Command)
				}
			} else if !r.Policy.AutoApprove {
				if !r.approve("run command", a.Command) {
					return fail("command denied by approval policy: %s", a.Command)
				}
			}
			timeout := 120 * time.Second
			if a.TimeoutMs > 0 {
				timeout = time.Duration(a.TimeoutMs) * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", "-c", a.Command)
			cmd.Dir = r.Workdir
			var buf bytes.Buffer
			cmd.Stdout = &buf
			cmd.Stderr = &buf
			err := cmd.Run()
			out := buf.String()
			if ctx.Err() == context.DeadlineExceeded {
				return fail("command timed out after %s\n%s", timeout, out)
			}
			if err != nil {
				return Result{Content: fmt.Sprintf("exit error: %v\n%s", err, out), IsError: true}, nil
			}
			return ok(out)
		},
	}
	r.tools["grep"] = Tool{
		Name:        "grep",
		Description: "Search file contents for a regexp pattern.",
		Parameters:  `{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"},"include":{"type":"string"}},"required":["pattern"]}`,
		Run: func(ctx context.Context, argsJSON string) (Result, error) {
			var a struct {
				Pattern string `json:"pattern"`
				Path    string `json:"path"`
				Include string `json:"include"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
				return fail("bad arguments: %v", err)
			}
			re, err := regexp.Compile(a.Pattern)
			if err != nil {
				return fail("bad pattern: %v", err)
			}
			root := r.Workdir
			if a.Path != "" {
				root = r.resolve(a.Path)
			}
			var hits []string
			walkErr := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				if a.Include != "" {
					m, _ := filepath.Match(a.Include, filepath.Base(p))
					if !m {
						return nil
					}
				}
				if info.Size() > 512*1024 {
					return nil
				}
				data, err := os.ReadFile(p)
				if err != nil {
					return nil
				}
				for i, line := range strings.Split(string(data), "\n") {
					if re.MatchString(line) {
						rel, _ := filepath.Rel(r.Workdir, p)
						hits = append(hits, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
						if len(hits) >= 200 {
							return filepath.SkipAll
						}
					}
				}
				return nil
			})
			_ = walkErr
			if len(hits) == 0 {
				return ok("no matches")
			}
			return ok(strings.Join(hits, "\n"))
		},
	}
	r.tools["glob"] = Tool{
		Name:        "glob",
		Description: "Find files by glob pattern relative to the workdir.",
		Parameters:  `{"type":"object","properties":{"pattern":{"type":"string"}},"required":["pattern"]}`,
		Run: func(ctx context.Context, argsJSON string) (Result, error) {
			var a struct {
				Pattern string `json:"pattern"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
				return fail("bad arguments: %v", err)
			}
			matches, err := filepath.Glob(filepath.Join(r.Workdir, a.Pattern))
			if err != nil {
				return fail("bad pattern: %v", err)
			}
			rel := make([]string, 0, len(matches))
			for _, m := range matches {
				rp, _ := filepath.Rel(r.Workdir, m)
				rel = append(rel, rp)
			}
			sort.Strings(rel)
			if len(rel) == 0 {
				return ok("no matches")
			}
			return ok(strings.Join(rel, "\n"))
		},
	}
	r.tools["git"] = Tool{
		Name:        "git",
		Description: "Run a git subcommand (status/diff/log/add/commit/…). Mutating commands need approval unless auto-approved.",
		Parameters:  `{"type":"object","properties":{"args":{"type":"array","items":{"type":"string"}}},"required":["args"]}`,
		Run: func(ctx context.Context, argsJSON string) (Result, error) {
			var a struct {
				Args []string `json:"args"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
				return fail("bad arguments: %v", err)
			}
			if len(a.Args) == 0 {
				return fail("no git args")
			}
			mutating := map[string]bool{"add": true, "commit": true, "push": true, "reset": true, "checkout": true, "clean": true, "restore": true}
			if mutating[a.Args[0]] && !r.Policy.AutoApprove {
				if !r.approve("git "+a.Args[0], strings.Join(a.Args, " ")) {
					return fail("git %s denied by approval policy", strings.Join(a.Args, " "))
				}
			}
			ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "git", a.Args...)
			cmd.Dir = r.Workdir
			var buf bytes.Buffer
			cmd.Stdout = &buf
			cmd.Stderr = &buf
			if err := cmd.Run(); err != nil {
				return Result{Content: fmt.Sprintf("git error: %v\n%s", err, buf.String()), IsError: true}, nil
			}
			return ok(buf.String())
		},
	}
	r.tools["plan"] = Tool{
		Name:        "plan",
		Description: "Manage the task plan: ops set_title (replace plan), add, done, clear.",
		Parameters:  `{"type":"object","properties":{"op":{"type":"string"},"title":{"type":"string"},"detail":{"type":"string"},"index":{"type":"integer"}},"required":["op"]}`,
		Run: func(ctx context.Context, argsJSON string) (Result, error) {
			var a struct {
				Op     string `json:"op"`
				Title  string `json:"title"`
				Detail string `json:"detail"`
				Index  int    `json:"index"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
				return fail("bad arguments: %v", err)
			}
			switch a.Op {
			case "add":
				if a.Title == "" {
					return fail("add needs a title")
				}
				r.plan.mu.Lock()
				r.plan.Steps = append(r.plan.Steps, PlanStep{Title: a.Title, Detail: a.Detail})
				r.plan.mu.Unlock()
			case "done":
				r.plan.mu.Lock()
				if a.Index < 0 || a.Index >= len(r.plan.Steps) {
					r.plan.mu.Unlock()
					return fail("done index out of range")
				}
				r.plan.Steps[a.Index].Done = true
				r.plan.mu.Unlock()
			case "clear":
				r.plan.mu.Lock()
				r.plan.Steps = nil
				r.plan.mu.Unlock()
			default:
				return fail("unknown plan op %q (add|done|clear)", a.Op)
			}
			r.plan.mu.Lock()
			defer r.plan.mu.Unlock()
			var b strings.Builder
			for i, s := range r.plan.Steps {
				mark := "[ ]"
				if s.Done {
					mark = "[x]"
				}
				fmt.Fprintf(&b, "%d. %s %s\n", i, mark, s.Title)
			}
			if len(r.plan.Steps) == 0 {
				return ok("plan is empty")
			}
			return ok(b.String())
		},
	}
}

// PlanSteps returns the current plan (for syncing into the session).
func (r *Registry) PlanSteps() []PlanStep {
	r.plan.mu.Lock()
	defer r.plan.mu.Unlock()
	return append([]PlanStep(nil), r.plan.Steps...)
}
