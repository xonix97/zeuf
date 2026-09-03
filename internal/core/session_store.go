package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionsDir returns the session storage directory (0600 files;
// conversations may contain sensitive work).
func SessionsDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "zeuf", "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "zeuf", "sessions")
}

// SanitizeID keeps session IDs filesystem-safe.
func SanitizeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) > 64 {
		s = s[:64]
	}
	if s == "" {
		s = "session"
	}
	return s
}

func sessionPath(id string) string {
	return filepath.Join(SessionsDir(), SanitizeID(id)+".json")
}

// SaveSession persists a session (0600). Callers set Meta["workdir"]
// beforehand so resume can warn on directory mismatch.
func SaveSession(s *Session) error {
	if s == nil || s.ID == "" {
		return fmt.Errorf("cannot save session without id")
	}
	s.touch()
	if err := os.MkdirAll(SessionsDir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sessionPath(s.ID), append(data, '\n'), 0o600)
}

// LoadSession reads a session by id. IDs are sanitized, so traversal is
// impossible.
func LoadSession(id string) (*Session, error) {
	data, err := os.ReadFile(sessionPath(id))
	if err != nil {
		return nil, fmt.Errorf("load session %q: %w", SanitizeID(id), err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse session %q: %w", SanitizeID(id), err)
	}
	if s.Meta == nil {
		s.Meta = map[string]string{}
	}
	return &s, nil
}

// SessionSummary is a session list row.
type SessionSummary struct {
	ID      string
	Task    string
	Updated time.Time
	Turns   int
	Models  []string
}

// ListSessions returns summaries, newest first. Corrupt files are skipped,
// never fatal.
func ListSessions() ([]SessionSummary, error) {
	dir := SessionsDir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []SessionSummary
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil || s.ID == "" {
			continue
		}
		turns := 0
		for _, m := range s.Messages {
			if m.Role == RoleUser {
				turns++
			}
		}
		out = append(out, SessionSummary{
			ID: s.ID, Task: s.Task, Updated: s.Updated,
			Turns: turns, Models: append([]string(nil), s.SwitchTrail...),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}
