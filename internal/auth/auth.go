// Package auth stores user credentials for connected backends.
// Primary storage is the OS credential mechanism (OS keychain via
// go-keyring); where unavailable it falls back to a 0600 JSON file under
// ~/.config/zeuf (same practice as gh/aws CLIs). The main config file
// never holds secrets — only references. Nothing here is ever logged.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/zalando/go-keyring"
)

// ErrNotFound is returned for missing entries.
var ErrNotFound = errors.New("auth: credential not found")

// Store persists secrets by (service, account).
type Store interface {
	Set(service, account, secret string) error
	Get(service, account string) (string, error)
	Delete(service, account string) error
}

// ServiceDirect is the service name for direct-endpoint API keys,
// keyed by endpoint name.
const ServiceDirect = "zeuf-direct"

// Path returns the fallback file path (ZEUF_AUTH_FILE overrides, for tests).
func Path() string {
	if p := os.Getenv("ZEUF_AUTH_FILE"); p != "" {
		return p
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "zeuf", "auth.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "zeuf", "auth.json")
}

// Open returns the best available store and its name ("keyring"|"file").
// It probes the OS keyring read-only; any failure selects the file store.
func Open() (Store, string) {
	if probeKeyring() {
		return KeyringStore{}, "keyring"
	}
	return NewFileStore(Path()), "file"
}

func probeKeyring() bool {
	_, err := keyring.Get("zeuf", "__probe__")
	return err == nil || errors.Is(err, keyring.ErrNotFound)
}

// KeyringStore keeps secrets in the OS credential manager.
type KeyringStore struct{}

// Set implements Store.
func (KeyringStore) Set(service, account, secret string) error {
	if err := keyring.Set(service, account, secret); err != nil {
		return fmt.Errorf("keyring set: %w", err)
	}
	return nil
}

// Get implements Store.
func (KeyringStore) Get(service, account string) (string, error) {
	s, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("keyring get: %w", err)
	}
	return s, nil
}

// Delete implements Store.
func (KeyringStore) Delete(service, account string) error {
	if err := keyring.Delete(service, account); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("keyring delete: %w", err)
	}
	return nil
}

// FileStore keeps secrets in a 0600 JSON file. Used only when the OS
// keyring is unavailable.
type FileStore struct {
	mu   sync.Mutex
	path string
}

// NewFileStore builds a file store at path.
func NewFileStore(path string) *FileStore { return &FileStore{path: path} }

func (f *FileStore) load() map[string]string {
	data, err := os.ReadFile(f.path)
	if err != nil {
		return map[string]string{}
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]string{}
	}
	return m
}

func (f *FileStore) save(m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f.path, append(data, '\n'), 0o600)
}

func key(service, account string) string { return service + "\x00" + account }

// Set implements Store.
func (f *FileStore) Set(service, account, secret string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.load()
	m[key(service, account)] = secret
	return f.save(m)
}

// Get implements Store.
func (f *FileStore) Get(service, account string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.load()[key(service, account)]
	if !ok {
		return "", ErrNotFound
	}
	return s, nil
}

// Delete implements Store.
func (f *FileStore) Delete(service, account string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.load()
	delete(m, key(service, account))
	return f.save(m)
}
