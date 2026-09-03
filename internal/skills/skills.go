// Package skills discovers SKILL.md playbooks from the user config dir
// and the workdir, and loads them into session context on demand.
// Format mirrors the opencode convention: <name>/SKILL.md with optional
// YAML frontmatter (name, description).
package skills

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skill is one discovered playbook.
type Skill struct {
	Name        string
	Description string
	Body        string
	Source      string // "user" or "project"
}

// Dirs returns candidate skill roots (missing dirs are fine).
func Dirs(workdir string) []struct {
	Path   string
	Source string
} {
	var out []struct {
		Path   string
		Source string
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		out = append(out, struct {
			Path   string
			Source string
		}{filepath.Join(xdg, "zeuf", "skills"), "user"})
	} else if home, err := os.UserHomeDir(); err == nil {
		out = append(out, struct {
			Path   string
			Source string
		}{filepath.Join(home, ".config", "zeuf", "skills"), "user"})
	}
	if workdir != "" {
		out = append(out, struct {
			Path   string
			Source string
		}{filepath.Join(workdir, ".zeuf", "skills"), "project"})
	}
	return out
}

// Discover lists skills newest... in name order across all roots.
func Discover(workdir string) []Skill {
	var out []Skill
	for _, d := range Dirs(workdir) {
		ents, err := os.ReadDir(d.Path)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(d.Path, e.Name(), "SKILL.md"))
			if err != nil {
				continue
			}
			name, desc, body := parseSkill(e.Name(), string(data))
			out = append(out, Skill{Name: name, Description: desc, Body: body, Source: d.Source})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Find returns a skill by name (case-insensitive).
func Find(workdir, name string) (Skill, bool) {
	for _, s := range Discover(workdir) {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return Skill{}, false
}

// parseSkill splits optional --- frontmatter from the body.
func parseSkill(dir, data string) (name, desc, body string) {
	name = dir
	body = strings.TrimSpace(data)
	if !strings.HasPrefix(body, "---") {
		return name, "", body
	}
	rest := body[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return name, "", body
	}
	for _, ln := range strings.Split(rest[:end], "\n") {
		ln = strings.TrimSpace(ln)
		if k, v, ok := strings.Cut(ln, ":"); ok {
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "name":
				if nv := strings.TrimSpace(v); nv != "" {
					name = nv
				}
			case "description":
				desc = strings.TrimSpace(v)
			}
		}
	}
	return name, desc, strings.TrimSpace(rest[end+4:])
}
