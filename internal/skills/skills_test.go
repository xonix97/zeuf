package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverAndFind(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	write := func(root, name, body string) {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "zeuf", "skills"), "review", "---\nname: review\ndescription: Review code for bugs\n---\n# Review\nLook closely.")
	write(filepath.Join(dir, "zeuf", "skills"), "plain", "# No frontmatter here.")
	work := t.TempDir()
	write(filepath.Join(work, ".zeuf", "skills"), "deploy", "---\ndescription: Ship it\n---\n# Deploy")

	got := Discover(work)
	if len(got) != 3 {
		t.Fatalf("skills = %+v", got)
	}
	// Sorted by name.
	if got[0].Name != "deploy" || got[1].Name != "plain" || got[2].Name != "review" {
		t.Errorf("order/names wrong: %+v", got)
	}
	if got[0].Source != "project" || got[2].Source != "user" {
		t.Errorf("sources wrong: %+v", got)
	}
	if got[2].Description != "Review code for bugs" || !strings.Contains(got[2].Body, "# Review") {
		t.Errorf("frontmatter wrong: %+v", got[2])
	}
	if got[1].Description != "" || !strings.Contains(got[1].Body, "No frontmatter") {
		t.Errorf("plain skill wrong: %+v", got[1])
	}
	s, ok := Find(work, "REVIEW")
	if !ok || s.Name != "review" {
		t.Errorf("case-insensitive find failed: %+v", s)
	}
	if _, ok := Find(work, "missing"); ok {
		t.Error("missing skill should not be found")
	}
}

func TestDiscoverMissingDirs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if got := Discover(filepath.Join(dir, "nowhere")); len(got) != 0 {
		t.Errorf("expected none, got %+v", got)
	}
}
