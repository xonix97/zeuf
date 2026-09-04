// Package cli — connect: attach new model backends to Zeuf.
//
// /connect (TUI + REPL) and `zeuf connect` share this core: presets for
// common providers, validation, and saving (config keeps the endpoint,
// secrets go to env or the auth store — never the config file).
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"zeuf/internal/auth"
	"zeuf/internal/config"
	"zeuf/internal/router"
)

// connectCmd implements `zeuf connect` (same wizard as /connect).
func connectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "connect",
		Short: "Attach a new model backend (OpenRouter, Ollama, custom, CLI logins)",
		RunE: func(cmd *cobra.Command, args []string) error {
			in := bufio.NewReader(os.Stdin)
			out := func(s string) { fmt.Print(s) }
			err := RunConnectREPL(cmd.Context(), in, out)
			if IsRescan(err) {
				fmt.Println("Rescan on next start; credentials untouched.")
				return nil
			}
			return err
		},
	}
}

// Preset is re-exported from config for wizard UIs.
type Preset = config.Preset

// Presets lists the supported connection kinds.
var Presets = config.Presets

// ConnectSpec is a completed wizard (UI-agnostic).
type ConnectSpec struct {
	Name    string
	Type    string
	BaseURL string
	// Exactly one of KeyEnv (use environment) or Secret (store now).
	KeyEnv string
	Secret string
	// StoreName reports where the secret went ("env:<VAR>"|"keyring"|"file"|"none").
	StoreName string
}

// Validate checks a spec without side effects.
func Validate(spec ConnectSpec) error {
	if strings.TrimSpace(spec.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.ContainsAny(spec.Name, " /") {
		return fmt.Errorf("name must not contain spaces or slashes")
	}
	u := strings.TrimSpace(spec.BaseURL)
	if u == "" || (!strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://")) {
		return fmt.Errorf("base URL must start with http:// or https://")
	}
	return nil
}

// Save persists the endpoint: config keeps name/URL/env-ref, the secret
// (when pasted) goes to the auth store. Returns the store used.
func Save(spec ConnectSpec) (string, error) {
	if err := Validate(spec); err != nil {
		return "", err
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	for _, d := range cfg.Direct {
		if d.Name == spec.Name {
			return "", fmt.Errorf("endpoint %q already exists (edit %s to change it)", spec.Name, config.Path())
		}
	}
	storeUsed := "none"
	keyEnv := strings.TrimSpace(spec.KeyEnv)
	if strings.TrimSpace(spec.Secret) != "" {
		store, name := auth.Open()
		if err := store.Set(auth.ServiceDirect, spec.Name, strings.TrimSpace(spec.Secret)); err != nil {
			return "", fmt.Errorf("store credential: %w", err)
		}
		storeUsed = name
		keyEnv = "" // stored key takes precedence; no env needed
	} else if keyEnv != "" {
		storeUsed = "env:" + keyEnv
	}
	epType := spec.Type
	if epType == "" && (spec.Name == "anthropic" || strings.Contains(spec.BaseURL, "anthropic.com")) {
		epType = "anthropic"
	}
	cfg.Direct = append(cfg.Direct, config.DirectEndpoint{Name: spec.Name, Type: epType, BaseURL: strings.TrimSpace(spec.BaseURL), APIKeyEnv: keyEnv})
	if err := config.Save(cfg); err != nil {
		return "", err
	}
	spec.StoreName = storeUsed
	return storeUsed, nil
}

// KeyStatus reports credential state for an endpoint without values.
func KeyStatus(ep config.DirectEndpoint) string {
	if ep.APIKeyEnv != "" {
		if os.Getenv(ep.APIKeyEnv) != "" {
			return "set (env " + ep.APIKeyEnv + ")"
		}
		return "missing (set " + ep.APIKeyEnv + ")"
	}
	store, _ := auth.Open()
	if store != nil {
		if _, err := store.Get(auth.ServiceDirect, ep.Name); err == nil {
			return "set (stored)"
		}
	}
	return "none configured (ok for keyless local endpoints)"
}

// RunConnectREPL runs the wizard on a plain terminal (REPL /connect and
// `zeuf connect`). Secrets are read without echo where possible.
func RunConnectREPL(ctx context.Context, in *bufio.Reader, out func(string)) error {
	out("Connect a model backend:\n")
	for i, p := range Presets {
		out(fmt.Sprintf("  %d) %s\n", i+1, p.Title))
	}
	loginOpen, loginKilo, loginGemini := fmt.Sprint(len(Presets)+1), fmt.Sprint(len(Presets)+2), fmt.Sprint(len(Presets)+3)
	out(fmt.Sprintf("  %s) OpenCode login / %s) Kilo login / %s) Gemini login (all in your own terminal)\n", loginOpen, loginKilo, loginGemini))
	choice := promptLine(in, out, fmt.Sprintf("Choice [1-%d]: ", len(Presets)+3))
	switch choice {
	case loginOpen:
		out("Run `opencode auth login` in your normal terminal, then press enter here to rescan.\n")
		_, _ = in.ReadString('\n')
		return errRescan{}
	case loginKilo:
		out("Run `kilo auth login` in your normal terminal, then press enter here to rescan.\n")
		_, _ = in.ReadString('\n')
		return errRescan{}
	case loginGemini:
		out("Gemini CLI login is end-of-life for the free tier; get a free key at AI Studio instead.\n")
		out("Add it via choice 2 (Gemini preset), then press enter here to rescan.\n")
		_, _ = in.ReadString('\n')
		return errRescan{}
	}
	var pre Preset
	for i, p := range Presets {
		if choice == fmt.Sprint(i+1) {
			pre = p
		}
	}
	if pre.ID == "" {
		return fmt.Errorf("unknown choice")
	}
	name := promptLine(in, out, fmt.Sprintf("Name [%s]: ", pre.ID))
	if name == "" {
		name = pre.ID
	}
	base := promptLine(in, out, fmt.Sprintf("Base URL [%s]: ", pre.BaseURL))
	if base == "" {
		base = pre.BaseURL
	}
	spec := ConnectSpec{Name: name, BaseURL: base}
	if pre.KeyEnv != "" || pre.ID == "custom" {
		out("Credential: (1) environment variable  (2) paste key now (stored securely)\n")
		how := promptLine(in, out, "Choice [1-2, default 1]: ")
		if how == "2" {
			secret := promptSecret("API key: ")
			if strings.TrimSpace(secret) == "" {
				return fmt.Errorf("empty key")
			}
			spec.Secret = secret
		} else {
			env := promptLine(in, out, fmt.Sprintf("Env var [%s]: ", pre.KeyEnv))
			if env == "" {
				env = pre.KeyEnv
			}
			if env == "" {
				env = strings.ToUpper(name) + "_API_KEY"
			}
			spec.KeyEnv = env
		}
	}
	used, err := Save(spec)
	if err != nil {
		return err
	}
	out(fmt.Sprintf("Connected %q (credential: %s). Discovering models…\n", name, used))
	return nil
}

// syncDirect registers saved direct endpoints missing from the live
// registry (after a /connect that added one mid-session).
func syncDirect(reg *router.Registry) {
	known := map[string]bool{}
	for _, n := range reg.Backends() {
		known[n] = true
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}
	for _, d := range cfg.Direct {
		if !known["direct:"+d.Name] {
			registerDirect(reg, d.Name)
		}
	}
}

// errRescan signals the caller to refresh model discovery.
type errRescan struct{}

func (errRescan) Error() string { return "rescan" }

// IsRescan reports whether err just means "refresh backends".
func IsRescan(err error) bool {
	_, ok := err.(errRescan)
	return ok
}

func promptLine(in *bufio.Reader, out func(string), prompt string) string {
	out(prompt)
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}
