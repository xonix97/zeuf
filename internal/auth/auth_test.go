package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreRoundtrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "auth.json")
	fs := NewFileStore(p)
	if _, err := fs.Get(ServiceDirect, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	if err := fs.Set(ServiceDirect, "x", "s3cr3t"); err != nil {
		t.Fatal(err)
	}
	got, err := fs.Get(ServiceDirect, "x")
	if err != nil || got != "s3cr3t" {
		t.Fatalf("got %q, %v", got, err)
	}
	if err := fs.Delete(ServiceDirect, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Get(ServiceDirect, "x"); !errors.Is(err, ErrNotFound) {
		t.Error("delete failed")
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("auth file perm = %o, want 600", st.Mode().Perm())
	}
}

func TestOpenFallsBackCleanly(t *testing.T) {
	// Must never error: keyring or file, one always works.
	s, name := Open()
	if s == nil || (name != "keyring" && name != "file") {
		t.Fatalf("Open() = %v, %q", s, name)
	}
}
