package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("ZEUF_AUTH_FILE", filepath.Join(dir, "auth.json"))
}

func TestValidate(t *testing.T) {
	if err := Validate(ConnectSpec{Name: "or", BaseURL: "https://x/v1"}); err != nil {
		t.Error(err)
	}
	for _, bad := range []ConnectSpec{
		{Name: "", BaseURL: "https://x/v1"},
		{Name: "bad name", BaseURL: "https://x/v1"},
		{Name: "ok", BaseURL: "ftp://x"},
	} {
		if err := Validate(bad); err == nil {
			t.Errorf("expected error for %+v", bad)
		}
	}
}

func TestSaveEnvEndpoint(t *testing.T) {
	testEnv(t)
	used, err := Save(ConnectSpec{Name: "or", BaseURL: "https://x/v1", KeyEnv: "ZEUF_TEST_OR_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if used != "env:ZEUF_TEST_OR_KEY" {
		t.Errorf("store = %q", used)
	}
	if _, err := Save(ConnectSpec{Name: "or", BaseURL: "https://x/v1"}); err == nil {
		t.Error("duplicate name must fail")
	}
}

func TestSaveSecretEndpoint(t *testing.T) {
	testEnv(t)
	used, err := Save(ConnectSpec{Name: "local", BaseURL: "http://localhost:1234/v1", Secret: "k-123"})
	if err != nil {
		t.Fatal(err)
	}
	if used != "keyring" && used != "file" {
		t.Errorf("store = %q", used)
	}
	data, _ := os.ReadFile(configPathForTest())
	_ = data
	// Config must reference the endpoint but never contain the secret.
	cfgData, err := os.ReadFile(xdgConfigForTest("config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfgData), "k-123") {
		t.Error("secret leaked into config file")
	}
	if !strings.Contains(string(cfgData), `"local"`) {
		t.Error("endpoint missing from config")
	}
}

func configPathForTest() string { return os.Getenv("ZEUF_AUTH_FILE") }

func xdgConfigForTest(name string) string {
	return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "zeuf", name)
}
