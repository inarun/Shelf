package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTOML(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_Valid(t *testing.T) {
	dir := t.TempDir()
	vault := filepath.Join(dir, "vault")
	books := filepath.Join(vault, "Books")
	if err := os.MkdirAll(books, 0o700); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "shelf.toml")
	writeTOML(t, cfgPath, `
[vault]
path = "`+filepath.ToSlash(vault)+`"
books_folder = "Books"
`)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if filepath.ToSlash(cfg.Vault.Path) != filepath.ToSlash(vault) {
		t.Errorf("Vault.Path got %q", cfg.Vault.Path)
	}
	if cfg.Vault.BooksFolder != "Books" {
		t.Errorf("Vault.BooksFolder got %q", cfg.Vault.BooksFolder)
	}
	if cfg.Server.Bind != "127.0.0.1" {
		t.Errorf("Server.Bind default: got %q", cfg.Server.Bind)
	}
	if cfg.Server.Port != 7744 {
		t.Errorf("Server.Port default: got %d", cfg.Server.Port)
	}
	if cfg.Providers.OpenLibrary.CacheTTLDays != 30 {
		t.Errorf("OpenLibrary.CacheTTLDays default: got %d", cfg.Providers.OpenLibrary.CacheTTLDays)
	}
	if cfg.Data.Directory == "" {
		t.Errorf("Data.Directory should default to BinaryDir() when unset")
	}
}

func TestLoad_NestedBooksFolder(t *testing.T) {
	dir := t.TempDir()
	vault := filepath.Join(dir, "vault")
	nested := filepath.Join(vault, "2 - Source Material", "Books")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "shelf.toml")
	writeTOML(t, cfgPath, `
[vault]
path = "`+filepath.ToSlash(vault)+`"
books_folder = "2 - Source Material/Books"
`)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Vault.BooksFolder != "2 - Source Material/Books" {
		t.Errorf("nested books_folder not preserved: got %q", cfg.Vault.BooksFolder)
	}
	if err := cfg.Validate(); err != nil {
		// Note: cfg.Validate uses net.Listen on the default port 7744 which may
		// be taken on some machines; swap in a free port first.
		cfg.Server.Port = freePort(t)
		if err := cfg.Validate(); err != nil {
			t.Errorf("nested books_folder should validate, got: %v", err)
		}
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err == nil {
		t.Fatal("expected ErrNoConfig")
	}
	if !errors.Is(err, ErrNoConfig) {
		t.Errorf("expected ErrNoConfig, got %v", err)
	}
}

func TestLoad_MalformedTOML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "shelf.toml")
	writeTOML(t, cfgPath, `this is = not valid [[[ toml`)
	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

func TestLoad_UnknownKeys(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "shelf.toml")
	writeTOML(t, cfgPath, `
[vault]
path = "C:/vault"
books_folder = "Books"

[typo_section]
something = true
`)
	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for unknown keys")
	}
	if !strings.Contains(err.Error(), "unknown keys") {
		t.Errorf("expected unknown-keys error, got: %v", err)
	}
}

func TestLoad_EmptyPathUsesBinaryDir(t *testing.T) {
	// Empty path resolves to {binary_dir}/shelf.toml. In a test binary,
	// no such file should exist, so ErrNoConfig is expected.
	_, err := Load("")
	if err == nil {
		t.Fatal("expected ErrNoConfig (no shelf.toml next to test binary)")
	}
	if !errors.Is(err, ErrNoConfig) {
		t.Errorf("expected ErrNoConfig, got %v", err)
	}
}

func TestLoad_ExplicitDataDirOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "data")
	if err := os.MkdirAll(explicit, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "shelf.toml")
	writeTOML(t, cfgPath, `
[vault]
path = "`+filepath.ToSlash(dir)+`"
books_folder = "Books"

[data]
directory = "`+filepath.ToSlash(explicit)+`"
`)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if filepath.ToSlash(cfg.Data.Directory) != filepath.ToSlash(explicit) {
		t.Errorf("Data.Directory: got %q want %q", cfg.Data.Directory, explicit)
	}
}
