package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/inarun/Shelf/internal/vault/paths"
)

// ValidationError aggregates every broken field from a single Validate call
// so the user sees all problems at once rather than fixing them one at a
// time. Errors is non-empty whenever Error() is non-nil.
type ValidationError struct {
	Errors []string
}

func (v *ValidationError) Error() string {
	if len(v.Errors) == 0 {
		return "config: no errors" // should not happen when returned non-nil
	}
	if len(v.Errors) == 1 {
		return "config: " + v.Errors[0]
	}
	return "config: " + strconv.Itoa(len(v.Errors)) + " validation errors:\n - " + strings.Join(v.Errors, "\n - ")
}

// Validate checks every field for correctness. It creates Data.Directory
// when absent (portable-mode first run) and proves writability by dropping
// and removing a marker file. All failures are collected into a single
// ValidationError so the user gets the full picture.
//
// External bind addresses do not produce an error here; callers should
// check IsExternalBind separately and log a warning per SKILL.md §Core
// Invariant #4.
func (c *Config) Validate() error {
	ve := &ValidationError{}

	validateVault(c, ve)
	validateData(c, ve)
	validateServer(c, ve)
	validateProviders(c, ve)
	validateRecommender(c, ve)

	if len(ve.Errors) > 0 {
		return ve
	}
	return nil
}

// IsExternalBind reports whether Server.Bind is something other than a
// loopback address. Main should log a warning when this is true.
func (c *Config) IsExternalBind() bool {
	switch strings.ToLower(c.Server.Bind) {
	case "", "127.0.0.1", "localhost", "::1":
		return false
	default:
		return true
	}
}

func validateVault(c *Config, ve *ValidationError) {
	switch {
	case c.Vault.Path == "":
		ve.push("vault.path is required")
	case !filepath.IsAbs(c.Vault.Path):
		ve.pushf("vault.path must be absolute, got %q", c.Vault.Path)
	default:
		info, err := os.Stat(c.Vault.Path)
		if err != nil {
			ve.pushf("vault.path %q: %v", c.Vault.Path, err)
		} else if !info.IsDir() {
			ve.pushf("vault.path %q is not a directory", c.Vault.Path)
		}
	}

	if err := paths.ValidateRelativeBooksFolder(c.Vault.BooksFolder); err != nil {
		ve.pushf("vault.books_folder: %v", err)
		return
	}
	if c.Vault.Path == "" || !filepath.IsAbs(c.Vault.Path) {
		return // no point resolving if vault.path is broken
	}
	abs := filepath.Join(c.Vault.Path, c.Vault.BooksFolder)
	info, err := os.Stat(abs)
	if err != nil {
		ve.pushf("vault.books_folder resolves to %q: %v", abs, err)
		return
	}
	if !info.IsDir() {
		ve.pushf("vault.books_folder resolves to %q which is not a directory", abs)
	}
}

func validateData(c *Config, ve *ValidationError) {
	switch {
	case c.Data.Directory == "":
		ve.push("data.directory is empty after defaults — BinaryDir() failed")
	case !filepath.IsAbs(c.Data.Directory):
		ve.pushf("data.directory must be absolute, got %q", c.Data.Directory)
	default:
		if err := os.MkdirAll(c.Data.Directory, 0o700); err != nil {
			ve.pushf("data.directory %q: cannot create: %v", c.Data.Directory, err)
			return
		}
		if err := probeWritable(c.Data.Directory); err != nil {
			ve.push(portableNotWritableError(c.Data.Directory, err))
		}
	}
}

func validateServer(c *Config, ve *ValidationError) {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		ve.pushf("server.port %d out of range 1..65535", c.Server.Port)
		return
	}
	addr := net.JoinHostPort(c.Server.Bind, strconv.Itoa(c.Server.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		ve.pushf("server.port: %s unavailable: %v", addr, err)
		return
	}
	_ = ln.Close()
}

func validateProviders(c *Config, ve *ValidationError) {
	if c.Providers.OpenLibrary.Enabled && c.Providers.OpenLibrary.CacheTTLDays < 1 {
		ve.pushf("providers.openlibrary.cache_ttl_days %d must be >= 1 when enabled",
			c.Providers.OpenLibrary.CacheTTLDays)
	}
	if c.Providers.Audiobookshelf.Enabled {
		if strings.TrimSpace(c.Providers.Audiobookshelf.BaseURL) == "" {
			ve.push("providers.audiobookshelf.base_url is required when enabled")
		}
		if strings.TrimSpace(c.Providers.Audiobookshelf.APIKey) == "" {
			ve.push("providers.audiobookshelf.api_key is required when enabled")
		}
		if c.Providers.Audiobookshelf.CacheTTLMinutes < 1 {
			ve.pushf("providers.audiobookshelf.cache_ttl_minutes %d must be >= 1 when enabled",
				c.Providers.Audiobookshelf.CacheTTLMinutes)
		}
	}
}

// allowedLLMModels is the startup allowlist of Anthropic model IDs
// this build will send requests to. Adding one is a spec change, not
// a config change (SKILL.md §Conventions for Claude Code). The lists
// are hardcoded to prevent a typo or deprecated model reaching the
// wire.
var allowedLLMModels = map[string]bool{
	"claude-sonnet-4-6": true,
	"claude-opus-4-7":   true,
	"claude-haiku-4-5":  true,
}

func validateRecommender(c *Config, ve *ValidationError) {
	if !c.Recommender.LLM.Enabled {
		return
	}
	if strings.TrimSpace(c.Recommender.LLM.APIKey) == "" {
		ve.push("recommender.llm.api_key is required when enabled")
	}
	m := strings.TrimSpace(c.Recommender.LLM.Model)
	if m == "" {
		ve.push("recommender.llm.model is required when enabled")
	} else if !allowedLLMModels[m] {
		ve.pushf(
			"recommender.llm.model %q is not in the allowlist (allowed: claude-sonnet-4-6, claude-opus-4-7, claude-haiku-4-5)",
			m,
		)
	}
}

func probeWritable(dir string) error {
	probe := filepath.Join(dir, ".shelf-writable-probe")
	// #nosec G304 -- `probe` is rooted in the config-validated data.directory
	// and uses a constant marker filename. The file is immediately removed
	// after a successful write; purpose is to prove writability at startup.
	f, err := os.OpenFile(probe, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte("probe")); err != nil {
		_ = f.Close()
		_ = os.Remove(probe)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(probe)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(probe)
		return err
	}
	return os.Remove(probe)
}

func portableNotWritableError(dir string, underlying error) string {
	return fmt.Sprintf(
		"data.directory %q is not writable (%v). Shelf runs in portable mode by default, which "+
			"stores shelf.db, covers/, backups/, and logs/ next to the binary. Either move the "+
			"binary to a user-writable location, or set data.directory in shelf.toml to an explicit "+
			"absolute path on a writable volume.",
		dir, underlying)
}

func (v *ValidationError) push(s string)                   { v.Errors = append(v.Errors, s) }
func (v *ValidationError) pushf(format string, a ...any) { v.Errors = append(v.Errors, fmt.Sprintf(format, a...)) }
