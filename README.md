# Shelf

Local-first reading journal for Obsidian users. Runs as a single Go binary
on Windows, keeps your Obsidian vault as the source of truth, and serves a
lightweight localhost UI for logging reading progress, ratings, and reviews.

**Status:** v0.3 shipped — Audiobookshelf sync, Trial Review System, and a rule-based recommender are in. See [SKILL.md](SKILL.md) for the full spec, priority order (security > lightweight > polish > features), and milestone plan.

## Quick start

1. **Build.** `go build -o shelf.exe ./cmd/shelf` (or `make build`).
2. **Configure.** Copy [`shelf.example.toml`](shelf.example.toml) to `shelf.toml` in the same directory as the binary and fill in `vault.path` + `vault.books_folder`.
3. **Run.** `./shelf.exe`. The tray icon appears, and your default browser opens to `http://127.0.0.1:7744/library`.
4. **Stop.** Right-click the tray icon → Quit, or Ctrl-C the terminal.

## Updating

Three options, pick whichever matches your workflow:

| I want…                                        | Do                                                                   |
| ---------------------------------------------- | -------------------------------------------------------------------- |
| One click from Explorer                        | Double-click [`update.bat`](update.bat). Stops, pulls, rebuilds, relaunches. |
| Same thing from a terminal                     | `make update` — same flow via the Makefile target.                   |
| Just see if anything's new without updating    | `make update-check` — fetches and lists unmerged commits.            |
| Full control                                   | Quit Shelf manually, `git pull`, `make build`, relaunch `./shelf.exe`. |

Windows can't overwrite a running `.exe`, so any update path has to stop the running instance first. The scripts force-kill it; all vault writes are atomic (temp + fsync + rename) and `shelf.db` uses SQLite WAL journaling, so kill-9 is crash-consistent. Config (`shelf.toml`), data (`shelf.db`, `covers/`, `backups/`, `logs/`), and Windows autostart registration all persist across updates.

## Priorities (non-negotiable)

1. **Security.** Path validation on every filesystem operation, atomic
   writes only, localhost-only by default, no external calls without user
   action.
2. **Lightweight.** Single Go binary, no cgo, no npm/bundler, stdlib-first.
3. **Polish.** Only after 1 and 2 are solid.
4. **Features.** Only after 1, 2, 3 are solid.

## Core invariants

- **Vault is truth.** The SQLite index is a rebuildable cache.
- **Atomic writes only.** Temp file + fsync + rename, always.
- **Explicit user fields always win.** External sources fill gaps; they
  never overwrite populated vault frontmatter.
- **Dry-run + backup before any bulk write.** No exceptions.

Full details in [SKILL.md](SKILL.md).
