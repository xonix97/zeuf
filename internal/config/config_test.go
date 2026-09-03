package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	d := Default()
	if !d.Prefs.FallbackEnabled || d.Prefs.MaxAttempts != 4 {
		t.Errorf("unsafe defaults: %+v", d.Prefs)
	}
	if len(d.BackendsOrder) == 0 {
		t.Error("no default backend order")
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfg := Default()
	cfg.Direct = []DirectEndpoint{{Name: "or", BaseURL: "https://x/v1", APIKeyEnv: "ZEUF_OR_KEY"}}
	cfg.Prefs.PinnedModel = "kilo/deepseek/deepseek-chat"
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Prefs.PinnedModel != cfg.Prefs.PinnedModel || len(loaded.Direct) != 1 {
		t.Errorf("roundtrip wrong: %+v", loaded)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "zeuf", "config.json"))
	if strings.Contains(string(data), "sk-") {
		t.Error("config file must never contain key material")
	}
}

func TestMissingFileGivesDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Prefs.Mode != "auto" {
		t.Errorf("mode = %q", cfg.Prefs.Mode)
	}
}
