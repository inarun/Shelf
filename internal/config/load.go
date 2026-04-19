package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ErrNoConfig is returned when the config file cannot be located at either
// the explicit --config path or the portable-mode default next to the
// binary.
var ErrNoConfig = errors.New("config file not found")

// Load parses shelf.toml from the given path. When path is empty, the
// portable-mode default ({binary_dir}/shelf.toml) is used.
//
// Defaults are applied after parsing: an empty Server.Bind becomes
// "127.0.0.1", an unset Server.Port becomes 7744, an unset
// Providers.OpenLibrary.CacheTTLDays becomes 30, and an empty
// Data.Directory is resolved to BinaryDir() for portable mode.
//
// Load does not call Validate; the caller is expected to call it
// immediately afterwards so that startup can fail loudly on any misconfig.
func Load(path string) (*Config, error) {
	if path == "" {
		binDir, err := BinaryDir()
		if err != nil {
			return nil, fmt.Errorf("config: resolve binary dir: %w", err)
		}
		path = filepath.Join(binDir, "shelf.toml")
	}

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w at %s", ErrNoConfig, path)
	}

	var cfg Config
	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		// Unknown keys are a signal the user has a typo — fail loudly
		// rather than ignoring them, per SKILL.md's "never start in a
		// degraded state" posture.
		return nil, fmt.Errorf("config: unknown keys in %s: %v", path, undecoded)
	}

	if err := applyDefaults(&cfg); err != nil {
		return nil, fmt.Errorf("config: apply defaults: %w", err)
	}
	return &cfg, nil
}

func applyDefaults(c *Config) error {
	if c.Server.Bind == "" {
		c.Server.Bind = "127.0.0.1"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 7744
	}
	if c.Providers.OpenLibrary.CacheTTLDays == 0 {
		c.Providers.OpenLibrary.CacheTTLDays = 30
	}
	if c.Providers.Audiobookshelf.CacheTTLMinutes == 0 {
		c.Providers.Audiobookshelf.CacheTTLMinutes = 15
	}
	if c.Data.Directory == "" {
		binDir, err := BinaryDir()
		if err != nil {
			return err
		}
		c.Data.Directory = binDir
	}
	return nil
}
