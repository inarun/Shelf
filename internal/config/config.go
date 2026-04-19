package config

import "path/filepath"

// Config is the complete TOML configuration for Shelf. Fields omitted in
// the TOML receive defaults during Load.
type Config struct {
	Vault       VaultConfig       `toml:"vault"`
	Data        DataConfig        `toml:"data"`
	Server      ServerConfig      `toml:"server"`
	Providers   ProvidersConfig   `toml:"providers"`
	Recommender RecommenderConfig `toml:"recommender"`
}

// VaultConfig points at the user's Obsidian vault and the Books subfolder.
// BooksFolder is a vault-relative path and may be nested (for example,
// "2 - Source Material\\Books").
type VaultConfig struct {
	Path        string `toml:"path"`
	BooksFolder string `toml:"books_folder"`
}

// DataConfig locates the SQLite index, cover cache, backups, and logs.
// Directory is optional: an empty value triggers portable mode, where the
// directory is the one containing the running binary.
type DataConfig struct {
	Directory string `toml:"directory"`
}

// ServerConfig controls the embedded HTTP server.
// Bind defaults to 127.0.0.1 and Port defaults to 7744 when unset.
type ServerConfig struct {
	Bind string `toml:"bind"`
	Port int    `toml:"port"`
}

// ProvidersConfig aggregates per-provider settings.
type ProvidersConfig struct {
	OpenLibrary    OpenLibraryConfig    `toml:"openlibrary"`
	Audiobookshelf AudiobookshelfConfig `toml:"audiobookshelf"`
}

// OpenLibraryConfig controls the Open Library metadata provider.
// Enabled is false by default; the user opts in explicitly.
type OpenLibraryConfig struct {
	Enabled      bool `toml:"enabled"`
	CacheTTLDays int  `toml:"cache_ttl_days"`
}

// AudiobookshelfConfig controls the Audiobookshelf sync provider.
// Enabled is false by default; the user opts in explicitly. BaseURL is
// the self-hosted AB server (http or https). APIKey is bearer-style
// and must never be checked into source control.
type AudiobookshelfConfig struct {
	Enabled         bool   `toml:"enabled"`
	BaseURL         string `toml:"base_url"`
	APIKey          string `toml:"api_key"`
	CacheTTLMinutes int    `toml:"cache_ttl_minutes"`
}

// RecommenderConfig controls the rule-based recommender (v0.3). Enabled
// is false by default so the debug endpoint /api/recommendations/profile
// returns 503 for users who have not opted in. The scorers themselves
// land in Session 18 and are gated on the same flag; the v0.3 UI arrives
// in Session 19. Purely a local computation — no outbound HTTP.
type RecommenderConfig struct {
	Enabled bool `toml:"enabled"`
}

// BooksAbsolutePath joins Vault.Path and Vault.BooksFolder into an absolute
// path. Callers that touch the filesystem must still run the result through
// internal/vault/paths validators — this is a config-level join, not a
// security check.
func (c *Config) BooksAbsolutePath() string {
	return filepath.Join(c.Vault.Path, c.Vault.BooksFolder)
}
