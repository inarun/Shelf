// Command shelf is the main entry point for the Shelf local-first reading
// journal. Session 1: loads the config, validates every path, warns on
// non-loopback bind addresses, and logs successful startup. Later sessions
// add the HTTP server, importer, index, and system tray.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/inarun/Shelf/internal/config"
)

func main() {
	configPath := flag.String("config", "", "path to shelf.toml (default: <binary_dir>/shelf.toml)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		if errors.Is(err, config.ErrNoConfig) {
			fmt.Fprintf(os.Stderr,
				"shelf: no config file found. Create shelf.toml next to the binary, "+
					"or point to one with --config <path>.\n  %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "shelf: %v\n", err)
		}
		os.Exit(1)
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "shelf: %v\n", err)
		os.Exit(1)
	}

	if cfg.IsExternalBind() {
		log.Printf("WARNING: server.bind = %q is not a loopback address. "+
			"Shelf is designed for localhost-only access. See SKILL.md §Core Invariants #4.",
			cfg.Server.Bind)
	}

	log.Printf("shelf starting: vault=%q books_folder=%q data=%q bind=%s:%d",
		cfg.Vault.Path, cfg.Vault.BooksFolder, cfg.Data.Directory,
		cfg.Server.Bind, cfg.Server.Port)
}
