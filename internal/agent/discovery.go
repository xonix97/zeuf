package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	ct "zeuf/internal/core/tools"
)

// Evidence holds repository facts gathered during the DISCOVERY phase.
type Evidence struct {
	Workdir          string   `json:"workdir"`
	Branch           string   `json:"branch"`
	PreExistingDirty []string `json:"pre_existing_dirty"`
	BuildSystem      string   `json:"build_system"`
	TestCommand      string   `json:"test_command"`
	KeyFiles         []string `json:"key_files"`
	RelevantFiles    []string `json:"relevant_files"`
	Summary          string   `json:"summary"`
}

// Discover probes the repository for build systems, pre-existing dirty state,
// and files relevant to the given task description.
func Discover(ctx context.Context, workdir string, task string, tools *ct.Registry) (*Evidence, error) {
	if workdir == "" {
		workdir, _ = os.Getwd()
	}

	ev := &Evidence{
		Workdir:       workdir,
		KeyFiles:      make([]string, 0),
		RelevantFiles: make([]string, 0),
	}

	// 1. Git branch and pre-existing dirty files
	branch, dirtyFiles := inspectGit(ctx, workdir)
	ev.Branch = branch
	ev.PreExistingDirty = dirtyFiles

	// 2. Build system and default verification command
	detectBuildSystem(workdir, ev)

	// 3. Targeted file retrieval based on task keywords
	if task != "" && tools != nil {
		ev.RelevantFiles = findRelevantFiles(ctx, workdir, task, tools)
	}

	// 4. Summarize evidence
	var b strings.Builder
	fmt.Fprintf(&b, "Workdir: %s\n", ev.Workdir)
	if ev.Branch != "" {
		fmt.Fprintf(&b, "Git Branch: %s (Dirty files before task: %d)\n", ev.Branch, len(ev.PreExistingDirty))
	}
	if ev.BuildSystem != "" {
		fmt.Fprintf(&b, "Build System: %s\n", ev.BuildSystem)
	}
	if ev.TestCommand != "" {
		fmt.Fprintf(&b, "Default Test Command: %s\n", ev.TestCommand)
	}
	if len(ev.RelevantFiles) > 0 {
		fmt.Fprintf(&b, "Candidate Relevant Files:\n")
		for _, f := range ev.RelevantFiles {
			fmt.Fprintf(&b, "  - %s\n", f)
		}
	}
	ev.Summary = strings.TrimRight(b.String(), "\n")
	return ev, nil
}

func inspectGit(ctx context.Context, workdir string) (branch string, dirty []string) {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	branchOut, err := exec.CommandContext(cctx, "git", "-C", workdir, "branch", "--show-current").Output()
	if err == nil {
		branch = strings.TrimSpace(string(branchOut))
	}

	statusOut, err := exec.CommandContext(cctx, "git", "-C", workdir, "status", "--porcelain").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(statusOut)), "\n")
		for _, ln := range lines {
			ln = strings.TrimSpace(ln)
			if ln == "" {
				continue
			}
			// line format: " M path" or "?? path"
			parts := strings.Fields(ln)
			if len(parts) >= 2 {
				dirty = append(dirty, parts[1])
			} else {
				dirty = append(dirty, ln)
			}
		}
	}
	return branch, dirty
}

func detectBuildSystem(workdir string, ev *Evidence) {
	checks := []struct {
		file   string
		system string
		test   string
	}{
		{"go.mod", "Go", "go test ./..."},
		{"package.json", "Node.js (npm)", "npm test"},
		{"Cargo.toml", "Rust (Cargo)", "cargo test"},
		{"pyproject.toml", "Python", "pytest"},
		{"pytest.ini", "Python (pytest)", "pytest"},
		{"Makefile", "Make", "make test"},
	}

	for _, c := range checks {
		p := filepath.Join(workdir, c.file)
		if _, err := os.Stat(p); err == nil {
			ev.KeyFiles = append(ev.KeyFiles, c.file)
			if ev.BuildSystem == "" {
				ev.BuildSystem = c.system
				ev.TestCommand = c.test
			}
		}
	}
}

var wordRe = regexp.MustCompile(`[a-zA-Z0-9_\-\.\/]{3,}`)

var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true,
	"this": true, "from": true, "have": true, "what": true, "where": true,
	"how": true, "when": true, "fix": true, "add": true, "edit": true,
	"make": true, "test": true, "code": true, "repo": true, "file": true,
}

func findRelevantFiles(ctx context.Context, workdir, task string, tools *ct.Registry) []string {
	words := wordRe.FindAllString(task, -1)
	var keywords []string
	for _, w := range words {
		lw := strings.ToLower(w)
		if !stopWords[lw] && len(w) >= 3 {
			keywords = append(keywords, w)
		}
	}

	seen := make(map[string]bool)
	var results []string

	// Direct path check: does the task mention a real file?
	for _, w := range words {
		cleaned := filepath.Clean(w)
		full := filepath.Join(workdir, cleaned)
		if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
			if !seen[cleaned] {
				seen[cleaned] = true
				results = append(results, cleaned)
			}
		}
	}

	// Targeted grep for distinctive keywords
	for _, kw := range keywords {
		if len(results) >= 10 {
			break
		}
		res, err := tools.Execute(ctx, "grep", fmt.Sprintf(`{"pattern":%q}`, regexp.QuoteMeta(kw)))
		if err == nil && !res.IsError && res.Content != "no matches" {
			lines := strings.Split(res.Content, "\n")
			for _, ln := range lines {
				colIdx := strings.Index(ln, ":")
				if colIdx > 0 {
					path := ln[:colIdx]
					if !seen[path] && !strings.Contains(path, ".git") {
						seen[path] = true
						results = append(results, path)
						if len(results) >= 10 {
							break
						}
					}
				}
			}
		}
	}

	return results
}
