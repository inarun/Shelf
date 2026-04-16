package config

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func synthSetup(t *testing.T) (vault, booksFolder, dataDir string) {
	t.Helper()
	root := t.TempDir()
	vault = filepath.Join(root, "vault")
	booksFolder = filepath.Join("2-src", "Books")
	if err := os.MkdirAll(filepath.Join(vault, booksFolder), 0o700); err != nil {
		t.Fatal(err)
	}
	dataDir = filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func baseValid(t *testing.T) *Config {
	vault, booksFolder, dataDir := synthSetup(t)
	return &Config{
		Vault:  VaultConfig{Path: vault, BooksFolder: booksFolder},
		Data:   DataConfig{Directory: dataDir},
		Server: ServerConfig{Bind: "127.0.0.1", Port: freePort(t)},
	}
}

func assertValidationError(t *testing.T, err error, wantSubstring string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", wantSubstring)
	}
	if !strings.Contains(err.Error(), wantSubstring) {
		t.Errorf("error %q does not contain %q", err.Error(), wantSubstring)
	}
}

func TestValidate_Valid(t *testing.T) {
	if err := baseValid(t).Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidate_MissingVaultPath(t *testing.T) {
	c := baseValid(t)
	c.Vault.Path = ""
	assertValidationError(t, c.Validate(), "vault.path is required")
}

func TestValidate_RelativeVaultPath(t *testing.T) {
	c := baseValid(t)
	c.Vault.Path = filepath.Join("relative", "vault")
	assertValidationError(t, c.Validate(), "must be absolute")
}

func TestValidate_NonExistentVaultPath(t *testing.T) {
	c := baseValid(t)
	c.Vault.Path = filepath.Join(c.Vault.Path, "does-not-exist")
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for non-existent vault path")
	}
}

func TestValidate_VaultPathIsFile(t *testing.T) {
	c := baseValid(t)
	file := filepath.Join(c.Vault.Path, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Vault.Path = file
	assertValidationError(t, c.Validate(), "is not a directory")
}

func TestValidate_BooksFolderAbsolute(t *testing.T) {
	// "/etc" is absolute on both POSIX and Windows (the latter since Go 1.21),
	// and has no drive letter so the "absolute path" error is the one that
	// surfaces rather than the drive-letter-specific message.
	c := baseValid(t)
	c.Vault.BooksFolder = "/etc"
	assertValidationError(t, c.Validate(), "must be vault-relative")
}

func TestValidate_BooksFolderTraversal(t *testing.T) {
	c := baseValid(t)
	c.Vault.BooksFolder = filepath.Join("..", "..", "etc")
	assertValidationError(t, c.Validate(), "escape the vault")
}

func TestValidate_BooksFolderDriveLetter(t *testing.T) {
	c := baseValid(t)
	c.Vault.BooksFolder = "D:other"
	assertValidationError(t, c.Validate(), "drive letter")
}

func TestValidate_BooksFolderUNC(t *testing.T) {
	c := baseValid(t)
	c.Vault.BooksFolder = `\\server\share`
	assertValidationError(t, c.Validate(), "UNC")
}

func TestValidate_BooksFolderNullByte(t *testing.T) {
	c := baseValid(t)
	c.Vault.BooksFolder = "Books\x00hidden"
	assertValidationError(t, c.Validate(), "null byte")
}

func TestValidate_BooksFolderNonExistent(t *testing.T) {
	c := baseValid(t)
	c.Vault.BooksFolder = "NotARealSubdir"
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for non-existent books folder")
	}
}

func TestValidate_DataDirCreatedIfMissing(t *testing.T) {
	c := baseValid(t)
	newDir := filepath.Join(filepath.Dir(c.Data.Directory), "brand-new-on-first-run")
	c.Data.Directory = newDir
	if err := c.Validate(); err != nil {
		t.Fatalf("expected validate to create missing data dir: %v", err)
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Errorf("data dir was not created: %v", err)
	}
}

func TestValidate_DataDirCannotBeCreated(t *testing.T) {
	c := baseValid(t)
	blocker := filepath.Join(filepath.Dir(c.Data.Directory), "blocker-file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Try to put a subdir inside a regular file — MkdirAll must fail.
	c.Data.Directory = filepath.Join(blocker, "impossible-subdir")
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error when data.directory cannot be created")
	}
	if !strings.Contains(err.Error(), "data.directory") {
		t.Errorf("expected data.directory error, got: %v", err)
	}
}

func TestValidate_PortOutOfRange(t *testing.T) {
	c := baseValid(t)
	c.Server.Port = 0
	assertValidationError(t, c.Validate(), "out of range")

	c.Server.Port = 70000
	assertValidationError(t, c.Validate(), "out of range")
}

func TestValidate_PortInUse(t *testing.T) {
	c := baseValid(t)
	addr := net.JoinHostPort(c.Server.Bind, strconv.Itoa(c.Server.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	assertValidationError(t, c.Validate(), "unavailable")
}

func TestValidate_OpenLibraryEnabledNegativeTTL(t *testing.T) {
	c := baseValid(t)
	c.Providers.OpenLibrary.Enabled = true
	c.Providers.OpenLibrary.CacheTTLDays = -5
	assertValidationError(t, c.Validate(), "cache_ttl_days")
}

func TestValidate_ExternalBindIsNotError(t *testing.T) {
	c := baseValid(t)
	c.Server.Bind = "0.0.0.0"
	c.Server.Port = freePort(t)
	if err := c.Validate(); err != nil {
		t.Errorf("external bind should not be a validation error, got: %v", err)
	}
	if !c.IsExternalBind() {
		t.Errorf("IsExternalBind should be true for 0.0.0.0")
	}
}

func TestIsExternalBind(t *testing.T) {
	cases := []struct {
		bind string
		want bool
	}{
		{"", false},
		{"127.0.0.1", false},
		{"localhost", false},
		{"LOCALHOST", false}, // case-insensitive
		{"::1", false},
		{"0.0.0.0", true},
		{"192.168.1.10", true},
		{"example.com", true},
	}
	for _, c := range cases {
		cfg := &Config{Server: ServerConfig{Bind: c.bind}}
		if got := cfg.IsExternalBind(); got != c.want {
			t.Errorf("IsExternalBind(%q) = %v, want %v", c.bind, got, c.want)
		}
	}
}

func TestValidate_MultipleErrorsAggregated(t *testing.T) {
	c := baseValid(t)
	c.Vault.Path = ""
	c.Vault.BooksFolder = ""
	c.Server.Port = 0
	err := c.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if len(ve.Errors) < 3 {
		t.Errorf("expected at least 3 errors, got %d: %v", len(ve.Errors), ve.Errors)
	}
}
