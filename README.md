# Shelf

Local-first reading journal for Obsidian users. Runs as a single Go binary
on Windows, keeps your Obsidian vault as the source of truth, and serves a
lightweight localhost UI for logging reading progress, ratings, and reviews.

**Status:** v0.1 in progress — see [SKILL.md](SKILL.md) for the full spec,
priority order (security > lightweight > polish > features), and milestone
plan.

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
