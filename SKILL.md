# Shelf — Project Skill Document

**Authoritative spec for the Shelf project.** Load this file at the start of every Claude Code session. Every architectural decision, filename, schema, and rule in this document is binding. If a user request contradicts this document, raise the contradiction and ask before proceeding.

Last updated: 2026-04-18 (Session 8 complete — inline SVG icon sprite, star-rating widget, keyboard shortcuts)

---

## What Shelf is

A local-first reading journal and library manager. It treats a user's Obsidian vault as the canonical data store (Markdown files with YAML frontmatter), maintains a SQLite index for fast querying, and serves a vanilla-JS PWA on localhost. It runs as a single Go binary on Windows, starts with the OS, lives in the system tray, and opens in a standalone app window.

The user (Nusayb) already uses Obsidian with the Book Search plugin's template. He has 123 books in a Goodreads export, 28 series tracked, writes substantive reviews irregularly, and wants a lower-friction surface for rating/reviewing/tracking that still leaves his vault as the source of truth.

## Priority order (non-negotiable)

**Security > Lightweight > Polish > Feature breadth.**

When decisions conflict, resolve in that order. Never trade security for any of the others. Never add weight (dependencies, processes, services) without a concrete need that can't be met otherwise.

## Core invariants

These cannot be violated without explicit user approval in conversation:

1. **Vault is truth.** The SQLite index is a rebuildable cache. If you delete the DB and re-scan the vault, you must end up with an equivalent state. Never store data in SQLite that isn't reflected in the vault.
2. **Atomic writes only.** Every file write goes through the atomic write primitive (temp file + fsync + rename). No exceptions, including "small" or "trusted" writes.
3. **Path validation on every filesystem operation.** Every path touched by the app must be validated against the configured Books folder. Reject path traversal, symlink escape, null bytes, reserved Windows names, and over-length paths.
4. **Localhost-only by default.** Network bind address is `127.0.0.1` unless the user explicitly configures otherwise. Even then, warn loudly.
5. **Explicit user fields always win.** Any field populated in a vault note beats any external source. External sources only fill gaps. This is the data precedence rule — see §Data Precedence.
6. **Dry-run before any bulk write.** Any operation that will modify more than one file must produce a preview and wait for explicit confirmation before writing.
7. **Pre-bulk-write backup.** Before any bulk write (the Goodreads importer, future mass migrations), snapshot the entire Books folder to a timestamped backup directory.
8. **No external calls without user action.** The app never "phones home." Metadata lookups happen only when the user explicitly adds a book or requests enrichment. No telemetry. Ever.

## Tech stack

- **Language:** Go, latest stable (1.23+ as of spec date)
- **Database:** SQLite via `modernc.org/sqlite` (pure-Go driver, no cgo) — chosen for single-binary distribution without cgo toolchain requirements
- **Config format:** TOML via `github.com/BurntSushi/toml`
- **YAML (frontmatter):** `gopkg.in/yaml.v3` at Node level (not high-level decoder), for preservation of field order and comments
- **HTTP:** Go standard library `net/http` with `http.ServeMux` (Go 1.22+ patterns). No web framework.
- **File watching:** `github.com/fsnotify/fsnotify`
- **Frontend:** Vanilla HTML/CSS/JS, no build step, no npm, no bundler. Embedded into the binary via `embed.FS`.
- **PWA:** Manifest + service worker, hand-written. No framework.
- **System tray (Windows):** `github.com/getlantern/systray` or equivalent. Evaluate alternatives before picking; must be actively maintained.

**Dependency policy:** Every third-party dependency is a security surface. Before adding one, justify it in a comment on the PR/commit and confirm it's actively maintained with no known CVEs. Standard library first, always.

## Architecture

Layered architecture. Higher layers depend on lower layers, never the reverse. Each package has a single responsibility and a `doc.go` stating it.

```
cmd/
  shelf/                  # main entry point — parses flags, loads config, wires dependencies
  
internal/
  config/                 # TOML config loading and validation
  
  vault/
    paths/                # path validation, filename generation, filename parsing
    atomic/               # atomic write + rename primitives (temp + fsync + rename)
    frontmatter/          # YAML frontmatter parse/serialize preserving order
    body/                 # body section parse/serialize (## headers as structure)
    note/                 # high-level: read/write/create a book note as a typed record
    watcher/              # fsnotify wrapper emitting typed events
    backup/               # recursive timestamped snapshot of the Books folder
    rename/               # non-canonical filename rename pipeline (plan + apply)

  strmatch/               # string normalization + Levenshtein (no external deps)
    
  domain/
    book/                 # Book type, validation, business rules
    series/               # Series detection and completion logic
    timeline/             # Reading timeline management (re-read aware)
    precedence/           # Data precedence resolver (vault vs. external sources)
    
  index/
    schema/               # SQLite schema + migrations (embedded SQL)
    store/                # CRUD operations against the index
    sync/                 # reconciles vault state into the index
    
  providers/
    metadata/             # interface: MetadataProvider
      openlibrary/        # implementation
      # hardcover/        # future
    reading/              # interface: ReadingSourceProvider  
      goodreads/          # CSV importer
      # audiobookshelf/   # future
      # kavita/           # future
      
  recommender/
    profile/              # taste profile extraction from index
    rules/                # rule-based scoring (v0.1)
    # llm/                # LLM-enhanced layer (future, optional)
    
  http/
    server/               # HTTP server setup, middleware chain
    middleware/           # CSRF, CSP, path validation, logging
    handlers/             # HTTP handlers, grouped by resource
    templates/            # Go html/template files (if any server-rendered content)
    static/               # embedded frontend assets (HTML/CSS/JS)
    
  tray/                   # system tray integration (Windows-specific behind build tags)
  
  platform/
    autostart/            # OS-specific autostart registration (Windows registry)
    browser/              # open URL in default browser, cross-platform
    
pkg/                      # anything genuinely reusable by third parties; start empty
tests/
  integration/            # end-to-end tests against synthetic vaults
  fixtures/               # test data including synthetic Goodreads exports
```

**Extension points — places future work plugs in without surgery:**

- `providers/metadata` and `providers/reading` are interfaces. Adding Hardcover, Audiobookshelf, or Kavita is a new package implementing the interface, not a modification of existing code.
- `recommender/rules` produces a scored list. Adding `recommender/llm` means adding a new scorer that composes with the rule-based scorer, not replacing it.
- `platform/` isolates OS-specific code. A future macOS/Linux port means new implementations behind build tags, not changes to business logic.
- `http/handlers` groups by resource, not by feature. Adding a "recommender" endpoint later is a new file in handlers, not a restructure.

## Filename convention

> **2026-04-16:** Replaced the original dual-convention (series prefix + dash separator) with a single canonical pattern, after verifying against Nusayb's real Books folder. Series identity now lives entirely in frontmatter; the filename never carries series info.

All book notes use:

```
{Title} by {Author}.md
```

Examples:
- `Hyperion by Dan Simmons.md`
- `Project Hail Mary by Andy Weir.md`
- `The Final Empire by Brandon Sanderson.md` (series identity is in frontmatter, not the filename)

**Rules:**

- Separator is ` by ` (single space, `by`, single space). Always.
- Series are not prefixed. A book's series name and position live in frontmatter fields (see §Frontmatter schema and §Open Questions for field-name resolution).
- Multiple authors: the filename uses the first author only. The full author list lives in frontmatter.
- **Non-canonical filenames** (files not matching `{Title} by {Author}.md` — e.g., `My Book - John Doe.md`) are indexed with a warning flag. The app never auto-renames. A rename pipeline with dry-run + per-file confirmation is planned for Session 2/3.

**Filename generation is centralized in `internal/vault/paths`.** Every caller uses that function. Future changes to the convention are a single-function edit.

**Filename sanitization:** Windows-reserved characters (`<>:"/\|?*`) are replaced with Unicode equivalents so titles remain readable.

| Char | Replacement | Codepoint |
| --- | --- | --- |
| `:` | `꞉` | U+A789 MODIFIER LETTER COLON |
| `?` | `？` | U+FF1F FULLWIDTH QUESTION MARK |
| `"` | `＂` | U+FF02 FULLWIDTH QUOTATION MARK |
| `<` | `＜` | U+FF1C FULLWIDTH LESS-THAN SIGN |
| `>` | `＞` | U+FF1E FULLWIDTH GREATER-THAN SIGN |
| `\|` | `｜` | U+FF5C FULLWIDTH VERTICAL LINE |
| `*` | `＊` | U+FF0A FULLWIDTH ASTERISK |
| `/` | `⁄` | U+2044 FRACTION SLASH |
| `\` | `⧵` | U+29F5 REVERSE SOLIDUS OPERATOR |

Runs of whitespace inside title/author are collapsed to a single space; leading/trailing whitespace is trimmed before sanitization.

**Filename parser:** splits on the **last** occurrence of ` by ` (titles can contain " by " — "Learning by Doing" — more commonly than author names do). Returns `ErrNonCanonical` if the pattern is absent; callers decide whether to index with a warning or skip. Round-trip property test: for every sanitized `(title, author)` pair, `Parse(Generate(t, a)) == (t, a)`.

## Frontmatter schema

> **2026-04-16 (Session 2):** Confirmed series field names from the Obsidian Book Search plugin template. `series` is a string; `series_index` is a number (supports fractional indices like 1.5). Filenames never carry series info per §Filename convention — series identity lives in frontmatter only.

The frontmatter matches the user's existing Obsidian Book Search plugin template exactly, with two additions for re-read tracking:

```yaml
tag: 📚Book
title: ""
subtitle: ""
authors: []              # array of author names
categories: []           # array of genre/category tags
series: ""               # series name, or empty if standalone
series_index:            # position in series (number; supports 1.5 etc.), or null
publisher: ""
publish: ""              # publish date as string (YYYY or YYYY-MM-DD)
total_pages: 
isbn: ""                 # ISBN-10 preferred, ISBN-13 acceptable
cover: ""                # local cache path or URL
format:                  # audiobook | ebook | physical | null
source:                  # where acquired: Audible, Libby, Library, etc. — freeform
started: []              # array of ISO dates — re-read aware
finished: []             # array of ISO dates — re-read aware, same length as started
rating: 
status: unread           # unread | reading | paused | finished | dnf
read_count: 0            # integer, derived from len(finished)
```

**Re-read handling:** `started` and `finished` are arrays, same length, paired by index. Rating is the *most recent* rating only — if the user re-rates on re-read, old rating is gone (the review text may reference past readings, but the structured field is latest). This is a deliberate simplification for v0.1. Future milestone may add `ratings: []` parallel array.

**Status state machine** (enforce in domain layer):

- `unread` → `reading` (sets `started[-1]` to today)
- `reading` → `paused` (no date change)
- `paused` → `reading` (no date change)  
- `reading` → `finished` (sets `finished[-1]` to today; increments `read_count`)
- `finished` → `reading` (re-read; appends new date to `started`)
- `reading` → `dnf` (no new `finished` date; does not increment `read_count`)
- `dnf` → `reading` (appends to `started`; user explicitly resuming)
- Any state → `unread` is forbidden without explicit user confirmation (it destroys read history)

## Body schema

The body of a book note has defined sections. The parser must preserve any sections it doesn't recognize (user-authored content is sacred). Recognized sections:

```markdown
# {Title}

Rating — {N}/5

## Key Ideas / Takeaways

- ...

## Notes

...

## Quotes & Highlights

...

## Actions

- ...

## Related

- [[Other Book]]

## Reading Timeline

- 2025-03-09 — Started reading (Kindle)
- 2025-03-14 — 120/604 pages (20%)
- 2025-04-02 — Finished, rated 4★
- 2025-11-15 — Started re-read (audiobook)
- 2025-12-30 — Finished re-read, rated 5★
```

**Reading Timeline is app-managed.** It's appended to on status transitions and progress updates. The user can edit it freely in Obsidian; the app parses it as structured history but does not reformat user edits unless there's a conflict.

Any heading the app doesn't recognize is preserved verbatim, in its original position. A body parser/serializer round-trip must be byte-equivalent for unrecognized content.

## Data precedence

> **2026-04-17 (Session 3):** Added a template-default exception for `status`. The Obsidian Book Search plugin template emits `status: unread` as its default; a strict read of the "populated" rule would block Goodreads import from ever setting status for template-created notes. The importer treats `status: unread` as a gap. Populated non-default statuses (`reading`, `paused`, `finished`, `dnf`) are preserved per the usual rule.

When a field value exists in multiple sources, resolution order from highest to lowest:

1. **Populated vault frontmatter** (user-authored or previously written by app)
2. **Populated vault body content** (for fields extracted from body, like review text from the Notes section)
3. **Goodreads CSV**
4. **Audiobookshelf** (future)
5. **Kavita** (future)
6. **Metadata provider** (Open Library, Hardcover) — lowest, used only for pure metadata (pages, publisher, cover) not personal data

**Rule:** external sources fill gaps, never overwrite. A field is "populated" if it's a non-zero, non-empty, non-null value. An empty string or empty array is a gap.

**`status` exception (Session 3):** `status: unread` is treated as a gap for the purpose of external-source import because it is the template default, not a deliberate user assertion. Any other populated status value (`reading`, `paused`, `finished`, `dnf`) is respected.

**Exception:** on explicit user action in the app UI ("pull latest from Goodreads," "re-fetch metadata"), the app may propose overwriting specific fields — but always with a diff preview and explicit confirmation.

## Concurrent edit handling

> **2026-04-16 (Session 2):** Windows-feasible flock without cgo is not practical for v0.1, so the staleness guard is a `(size, mtime_ns)` pair rather than mtime alone. The pair is stamped at read time via a single `os.File.Stat` on the open handle (eliminates the read-stat race) and re-checked immediately before the atomic rename. NTFS mtime resolution is coarse enough on Windows that same-ns collisions are possible across fast edits; comparing size as well catches the common miss. The tiny stat→rename race window is accepted for v0.1. Frontmatter-only writes remain unconditional — they re-read the file from disk, replace only the delimited YAML block, and write atomically, so they cannot clobber concurrent body edits.

Obsidian and Shelf can both have the same file open. The rules:

**Frontmatter-only writes (rating change, status change, date stamp):** always safe, always write. Read current file, mutate only the targeted frontmatter fields, write atomically. Even if Obsidian is editing the body, this won't clobber body content because the app only replaces the frontmatter block.

**Body writes (full review edit, timeline update):** check the `(size, mtime_ns)` staleness pair before writing. If either value differs from the pair stamped at read time, **refuse the write** with a clear error: "This note was changed outside the app. Reload before saving." The UI must provide a "Reload" button. Never attempt automatic merging.

**Implementation detail:** the app's in-memory book record stamps `(size, mtime_ns)` from `os.File.Stat` on the open handle at read time. The write path re-stats the target immediately before rename and aborts with `ErrStale` if either stamped value differs.

## Configuration

> **2026-04-17:** `providers.openlibrary.enabled` is now consulted at runtime — previously it was validated but the provider was wired unconditionally. A `false` value (the struct zero-value, i.e. the literal first-boot default if the TOML omits the section) means `cmd/shelf/main.go` passes a nil `metadata.Provider` to the HTTP server; the `/add` page renders a "provider not configured" banner and `/api/add/*` endpoints return 503. This gives Core Invariant #8 ("no external calls without user action") a stronger floor: a user who has not opted in cannot trigger outbound HTTP at all, even by clicking around. `shelf.example.toml` now enables the provider explicitly so users who copy the example get the working add-book flow.
>
> **2026-04-16:** Default config location and `data.directory` default changed. Shelf now runs in *portable mode* by default — config and data live next to the binary. Also: `books_folder` may be a nested vault-relative path, not just a flat subfolder name.

Config file is TOML. Resolution order for locating it at startup:

1. Path passed via `--config` flag (explicit override)
2. `{binary_dir}\shelf.toml` (portable mode default, where `binary_dir` is the directory containing `shelf.exe`, resolved via `os.Executable()`)

If neither exists, startup fails with a clear error telling the user where to put `shelf.toml`.

```toml
[vault]
path = "C:\\Users\\nusay\\Documents\\personal\\obs\\adwa"
books_folder = "2 - Source Material\\Books"   # vault-relative path; may be nested

[data]
# directory is optional in portable mode; defaults to the binary's directory.
# Uncomment to override with an explicit absolute path:
# directory = "D:\\Shelf-data"
# Contains: shelf.db (SQLite), covers/ (cached images), backups/ (pre-bulk-write snapshots), logs/

[server]
bind = "127.0.0.1"              # localhost-only by default
port = 7744

[providers.openlibrary]
enabled = true                   # opt-in to outbound metadata/cover lookups; false ⇒ add-book disabled
cache_ttl_days = 30

# Future sections, all disabled by default:
# [providers.hardcover]
# [providers.audiobookshelf]
# [providers.kavita]
# [recommender.llm]
```

**Config validation on startup:**

- Every path is absolute after resolution and exists (`vault.path`, resolved `data.directory`).
- `books_folder` is a vault-relative path: no absolute path, no `..`, no drive letter, no UNC prefix. Joined against `vault.path`, it must exist and be a directory.
- `data.directory` (default: binary's directory in portable mode) must be writable — verified by creating and removing a marker file. If not writable, Shelf fails loudly with an actionable error pointing at either moving the binary to a user-writable location or setting `data.directory` to an explicit external path. **Never starts in a degraded state.**
- `server.port` must not be in use.
- `server.bind` defaults to `127.0.0.1`; warn (don't error) if set to `0.0.0.0` or an external interface — matches §Core Invariant #4.

## Goodreads CSV import

> **2026-04-17 (Session 3):** Operationalized. Concrete handling captured below: Excel formula ISBN format `="..."` is stripped; dates parse as `YYYY/MM/DD` first and fall back to `YYYY-MM-DD`; titles of the form `Title (Series Name, #N)` split into clean title + series + index (fractional indices like `#1.5` supported); `Bookshelves` column populates `categories` after filtering out the three exclusive-shelf values (`to-read`, `currently-reading`, `read`) which are status-mapping only; multi-author handling combines `Author` and `Additional Authors` (comma-split) into the `authors` array; fuzzy match threshold is Levenshtein ratio ≥ 0.92 on normalized title AND surname exact-normalized match (softer matches become conflicts); `status: unread` is a gap (see §Data precedence exception); review text is written into the `## Notes` section with `_Imported from Goodreads on YYYY-MM-DD_` provenance, every review line blockquote-prefixed (`> `) so a stray `## ` in a review cannot accidentally start a new body section. Apply is sequential, sorted by filename, and accumulates per-entry errors into a report — the backup IS the rollback.

The single most dangerous operation in v0.1 (touches ~100 files). Design rules:

**Match order for existing notes:**
1. Exact ISBN13 match
2. Exact ISBN10 match
3. Fuzzy title + author match (Levenshtein distance on normalized strings, high threshold, conservative)

**Status mapping from Goodreads exclusive shelf:**
- `to-read` → `unread`
- `currently-reading` → `paused` (user explicitly requested this — he has 10 stalled books on the shelf)
- `read` → `finished`

**Rating:** Goodreads `0` means unrated; leave the field empty. 1-5 maps directly.

**Date mapping:**
- `Date Read` → `finished[0]` if status is `finished`
- `Date Added` → informational, logged in Reading Timeline section as "Added to shelf"
- No date from CSV populates `started` — we don't have that data

**Review text:** if present, goes under `## Notes` in body. Prepend a line `_Imported from Goodreads on YYYY-MM-DD_` for provenance.

**Dry run output:** produces a JSON report:
```json
{
  "will_create": [{"filename": "...", "reason": "no matching note found", "preview": "..."}],
  "will_update": [{"filename": "...", "reason": "ISBN13 match", "changes": [{"field": "rating", "old": null, "new": 4}]}],
  "will_skip": [{"filename": "...", "reason": "all fields already populated"}],
  "conflicts": [{"filename": "...", "reason": "title fuzzy match but authors differ", "needs_user_decision": true}]
}
```

The UI renders this as a review screen. Nothing is written until user clicks "Apply." Before applying, the full Books folder is copied to `{data.directory}\backups\books-{timestamp}\`.

## Security controls

**Required controls for v0.1:**

- **Path validation** on every filesystem operation (see §Core Invariants)
- **Atomic writes only** (see §Core Invariants)
- **CSRF protection:** SameSite=Strict session cookie + per-request CSRF token on all state-changing endpoints (POST/PUT/PATCH/DELETE)
- **CSP headers:** `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'`. No inline scripts. No external resources.
- **DNS rebinding protection:** validate `Host` header on every request; accept only `localhost:{port}`, `127.0.0.1:{port}`, and explicitly configured bind addresses
- **Input validation:** every frontmatter field has a validator (e.g., ISBN format, rating range 1-5, status enum). Reject invalid input at the API boundary.
- **File permissions:** SQLite DB and config file are created with user-only permissions (0600 on Unix; equivalent ACL on Windows)
- **No shell execution** anywhere. Go's `os/exec` may be used only with fully literal argument slices, never with user input.
- **Logging:** log to file only, never to stdout in production. Logs never contain file contents or personally-identifiable text from the user's reviews. Log redaction for secrets (none in v0.1, but the policy is set now).

**Security tooling to run before every commit:**

- `go vet`
- `staticcheck`
- `gosec`
- `govulncheck`

Makefile targets wire all four. CI (if set up) runs all four and blocks on failure.

## Testing policy

- **Unit tests** for every package with non-trivial logic. Target: anything that isn't a thin wrapper.
- **Table-driven tests** for parsers, validators, and filename utilities.
- **Property-based tests** (via `testing/quick` or `gopter`) for round-tripping: parse(serialize(x)) == x, and vice versa, on generated random inputs.
- **Integration tests** in `tests/integration/` that exercise the full flow against a synthetic vault constructed in a temp directory. Tests must clean up after themselves and never touch real user data.
- **Hostile test cases** for security-critical code: path traversal attempts, null bytes, oversized inputs, concurrent writes, malformed YAML, malformed Markdown, file locks, disk full (simulated), permission denied (simulated).
- **No test shall ever write to the configured vault path.** Tests use `t.TempDir()` for filesystem work.
- Coverage is informational, not a gate. Don't chase coverage numbers; chase test quality.

## Milestones

### v0.1 — The foundation (current target)

**Goal:** user can run the binary, import his Goodreads CSV safely, and view/edit his library through the web UI.

Session 1 (scaffolding):
- Config, path utilities, atomic writes, frontmatter parser — and their tests

Session 2 (vault round-trip):
- Body parser, note-level read/write, SQLite index schema and sync, fsnotify watcher

Session 3 (import):
- Goodreads CSV parser, matching logic, dry-run report generator, backup utility, apply path

Session 4 (HTTP + UI):
- HTTP server with middleware, handlers for library/book/import, static asset embedding, basic HTML UI for library view and book detail

Session 5 (Windows integration):
- System tray, autostart registration, browser open, PWA manifest and service worker

Session 6 (polish) — **complete as of 2026-04-17**:
- Add-book flow with Open Library, cover caching, series view, stats page, procedural PWA/tray icons

### v0.1.1 — Design polish (post-ship, 2026-04-17+)

Post-v0.1 design arc. Each session ships as a tagged patch release and is scoped to a single focused sitting (4–6 hours).

Session 7 (design foundation) — **complete as of 2026-04-17**:
- CSS design tokens (spacing/radius/shadow/motion/color/type) in `internal/http/static/app.css`
- Universal `:focus-visible` ring, hover/active transitions on every button/link/input
- Button + form input polish, `data-busy` spinner glyph
- Toast notification system (bottom-right, `aria-live="polite"`) replacing most `showBanner` calls
- CSP-compliant bar chart (`barWidthClass` template helper → `.bar--wN` utility classes) and import form layout (named classes, zero inline `style=""`)
- Cool-minimal monochrome + single-accent palette (Linear/Raycast/Vercel-adjacent)
- Service worker `CACHE_VERSION` bump so returning clients get the new bundle
- Fixes "buttons appear to do nothing" by making every async action emit a toast + disabling the button + showing the spinner glyph for the duration

Session 8 (icons + interaction) — **complete as of 2026-04-18**:
- `internal/http/templates/_sprite.html` defines an `iconSprite` partial holding a zero-sized `<svg>` with 11 `<symbol>` definitions — star-filled, star-outline, book, search, plus, refresh, chevron-right, check, x, keyboard, spinner. `_shared.html`'s `nav` partial emits it once per page so every page body carries the sprite with a single edit. Consumers call `<svg class="icon"><use href="#icon-..."/></svg>`; `fill: currentColor` drives color from each button's own color rule.
- Star-icon rating widget in `book_detail.html`: five buttons of class `rating-star`, each with a `<use href="#icon-star-filled"/>` plus an accessible `aria-label="N star(s)"`. CSS maps `color: var(--border-strong)` (off) → `var(--star)` (hover or `aria-pressed="true"`), so one sprite symbol covers both visual states.
- Keyboard shortcuts in `initShortcuts()` (`app.js`): `/` focuses the first filter/search input; `g l|s|a|i` within 600 ms navigates to `/library`/`/series`/`/add`/`/import`; `?` toggles the `#kbd-help` dialog; `Esc` closes the dialog or blurs the current input. Shortcuts are ignored when the event target is an `<input>`/`<textarea>`/`<select>` or any `contentEditable` element, and any modifier key (Ctrl/Meta/Alt) cancels the capture.
- Help overlay (`helpOverlay` partial in `_shared.html`): `<div id="kbd-help" role="dialog" aria-modal="true">` with a muted backdrop and a panel listing every chord. Any element with `data-kbd-help-dismiss` (backdrop + close button) closes it. A keyboard-icon button (`#kbd-help-btn`) appears at the right edge of the top nav for mouse users.
- Optimistic rating: `paint(next)` flips `aria-pressed` on every button synchronously, the PATCH fires, and `paint(prev)` reverts on failure. Toasts remain only on the API reply so success/error wording still matches reality.
- Converted the three remaining JS `.style.*` assignments in `app.js` to class toggles: the per-conflict row uses `.diff-conflict-row` (padding + red-tinted background now in CSS), empty-section text uses `.muted`, and each conflict's radio label uses `.conflict-radio`. Zero `.style.*` assignments remain in `app.js`.
- `sw.js` `CACHE_VERSION` bumped `shelf-v2 → shelf-v3` so returning clients pick up the new sprite + JS bundle on next activation.
- Regression guards in `templates_test.go`: `TestNavEmitsIconSpriteAndHelpOverlay` asserts that `nav` renders the star/keyboard/x symbols plus the `#kbd-help` dialog shell on every page; `TestBookDetailRatingUsesStarIcons` asserts that rating buttons carry `class="rating-star"`, a `#icon-star-filled` `<use>`, and pluralized `aria-label`s. `TestNoInlineStyleAttributesInTemplates` continues to fail the build on any regression to `style=""`.

Session 9 (empty states + a11y):
- Empty-state designs with inline SVG illustrations for library, series, stats, and timeline sections
- Bar-chart width-in animation (`requestAnimationFrame`-gated, respects `prefers-reduced-motion`)
- Full `axe-core` a11y audit (run via in-page console snippet, no dependency install); fix all findings including fieldset+legend on rating, `<label>` density, skip-to-content link
- Contrast verification tool at `cmd/a11y-check/main.go` (build tag `ignore`) — parses `--*` custom properties from app.css and reports WCAG 2.2 AA pass/fail for every (fg, bg) pair

Session 10 (typography + motion system):
- Display font-stack refinement; letter-spacing pass on all display headings; `font-feature-settings` for Segoe UI Variable stylistic alternates
- Tabular numerics on every numeric cell site-wide
- Coordinated motion language: per-card stagger on grid loads, `<main>` fade-in, tuned button `:active` scale
- SVG logo + wordmark replacing the text brand link in `_shared.html`
- Design system captured as a new §Design system section in SKILL.md between §Configuration and §Goodreads CSV import

### v0.2 — Audiobookshelf sync (future)

Read-only sync of listening progress into Reading Timeline entries. Implements `providers/reading/audiobookshelf`. Data precedence per §Data Precedence.

### v0.3 — Recommender (future)

Rule-based scorer in `recommender/rules`. Taste profile from the index. Series-completion prioritization, author affinity, shelf similarity, length/genre matching.

### v0.4 — Kavita sync (future)

Same pattern as Audiobookshelf.

### v0.5 — LLM-enhanced recommender (future)

Opt-in, bring-your-own-Anthropic-API-key. Lives in `recommender/llm`. Never called unless user explicitly triggers it. Configured in its own TOML section. Composes with the rule-based scorer, not replacing it. Sends review text and metadata only; never telemetry.

### v0.6 — Hardcover metadata provider (future)

Alternative to Open Library. Better series metadata. Implements existing `providers/metadata` interface.

### v0.7 — Mobile/Tailscale access (future)

Requires: Host header validation extended to allow Tailscale addresses; PWA layout hardening for mobile; maybe service worker offline improvements.

## Conventions for Claude Code

- **Read `SKILL.md` at the start of every session.** Confirm you have, out loud, in the first message.
- **Ask clarifying questions in batches**, not one at a time. The user has explicitly requested this.
- **Surface contradictions rather than resolve them silently.** If a request contradicts this spec, say so.
- **Update this file** when decisions are made that change specs. Include a dated note at the top of the changed section.
- **Every new dependency** requires a justification comment and a check against known CVEs.
- **Every security-sensitive change** requires hostile test cases before merge.
- **Never write to the real vault from tests or from development builds.** Development config points to a synthetic vault under `tests/fixtures/`.
- **Small commits, clear messages.** Commit message format: `{area}: {what changed} — {why}`. Example: `vault/paths: reject reserved Windows names — prevents creating inaccessible files`.
- **Windows is the primary target.** Code runs on Windows in production. Cross-platform concerns (macOS/Linux) are secondary and handled via build tags.
- **Don't chase features beyond the current milestone.** Leave seams (interfaces, config sections) for future work, but don't half-build them.

## Anti-patterns — things that will be rejected in review

- Any direct filesystem call that doesn't go through `internal/vault/`
- Any YAML field addition that isn't in this spec
- Any external HTTP call from a path not explicitly triggered by user action
- Any filename generation not going through `internal/vault/paths`
- Any test that writes outside `t.TempDir()`
- Any dependency added without justification
- Any inline script, external CDN, or non-self-origin resource in the frontend
- Any inline `style=""` attribute in a template — blocked at runtime by the strict `style-src 'self'` CSP. Use a named CSS class or a template helper that emits class tokens (see `barWidthClass` in `internal/http/templates/templates.go`). `TestNoInlineStyleAttributesInTemplates` enforces this.
- Any `panic()` in production code paths (tests are fine); errors bubble up with context
- "TODO: improve later" without a GitHub issue or at minimum a `FUTURE.md` entry
- Silent error swallowing — every error is either handled meaningfully or returned with context

## Open questions (to resolve as they come up)

- ~~**Filename convention verification:**~~ *Resolved 2026-04-16.* Convention is `{Title} by {Author}.md` with no series prefix. See §Filename convention.
- ~~**Non-canonical filename rename pipeline:**~~ *Resolved 2026-04-17 (Session 3).* Delivered as `internal/vault/rename`, a separate package with its own Plan / Apply that shares the backup + atomic-rename primitives with the Goodreads importer. Session 2's `canonical_name = 0` flag is the rename pipeline's input; it scans those rows and proposes a rename to the canonical form derived from frontmatter title + authors[0].
- ~~**Series frontmatter fields:**~~ *Resolved 2026-04-16 (Session 2).* `series` (string) + `series_index` (number). See §Frontmatter schema.
- ~~**Windows autostart mechanism:**~~ *Resolved 2026-04-17 (Session 5).* `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` — per-user, no elevation, no Task Scheduler dependency. Implementation in `internal/platform/autostart` via `golang.org/x/sys/windows/registry`. User controls enable/disable through the tray menu only (no TOML flag); the registry value is the current binary path, quoted, with the original `--config` flag preserved if the user launched with one.
- ~~**System tray library selection:**~~ *Resolved 2026-04-17 (Session 5).* Direct Win32 via `syscall.NewLazyDLL` + `golang.org/x/sys/windows` — no third-party tray dependency. Implementation in `internal/tray` behind `//go:build windows`, with `tray_other.go` returning `ErrNotSupported` on non-Windows so `cmd/shelf` runs headless in dev. Menu: Open Shelf / Start with Windows (checkable) / Quit. Icon is stock `IDI_APPLICATION` for v0.1; custom .ico deferred to Session 6.
- ~~**PWA icon assets:**~~ *Resolved 2026-04-17 (Session 6).* Raster icons are generated procedurally from pure geometry (rounded-square accent-color background with three stacked "shelf + book spines" bands) by `cmd/gen-icons/main.go` — a one-shot utility tagged `//go:build ignore` so `go build ./...` skips it. It writes `internal/http/static/icon-192.png` + `icon-512.png` (embedded + listed in `manifest.webmanifest` as `purpose: any maskable`) and `internal/tray/icon.ico` (16/32/48 PNG-embedded entries). The tray loads the ICO bytes via `//go:embed`, calls `LookupIconIdFromDirectoryEx` + `CreateIconFromResourceEx` at the system `SM_CXSMICON` metric, and falls back to stock `IDI_APPLICATION` if the embed parse fails. Run `go run cmd/gen-icons/main.go` to regenerate.
- **Single-instance semantics** (added 2026-04-17, Session 5): a second `shelf.exe` launch probes `127.0.0.1:<port>/healthz` for the Shelf signature (`HealthSignature` = `"shelf ok"`); if found, opens `/library` in the default browser and exits 0. Otherwise it starts as primary. No named-mutex lock; a genuine port collision with an unrelated service still produces a clear bind error on startup.
- ~~**`providers.openlibrary.enabled` wiring:**~~ *Resolved 2026-04-17 (post-v0.1 polish).* The flag is honored in `cmd/shelf/main.go`: when false, `olClient` stays nil and the HTTP server receives a nil `metadata.Provider`. Handlers in `internal/http/handlers/add.go` already return 503 on a nil provider and the add-page template already renders a "provider not configured" banner via `ProviderWired`, so no handler-side changes were needed. Motivation is Core Invariant #8: a user who hasn't opted in has zero outbound HTTP surface area, not merely "no trigger yet." `shelf.example.toml` now has an uncommented `[providers.openlibrary]` section with `enabled = true` so copying the example produces a working add-book flow; a user who wants the no-phone-home posture comments the section out or sets `enabled = false`.
- **Open Library contract** (added 2026-04-17, Session 6): Shelf hits exactly two hosts — `openlibrary.org` for metadata and `covers.openlibrary.org` for cover images. `LookupByISBN` uses `/api/books?bibkeys=ISBN:<n>&format=json&jscmd=data` (author names resolved inline, no follow-up OLID fetch). `Search` uses `/search.json?q=<q>&limit=10&fields=key,title,author_name,first_publish_year,isbn,cover_i`. Covers use `/b/{id|olid|isbn}/<value>-L.jpg?default=false` so "no cover" returns 404 instead of a placeholder. Every request enforces a 15s timeout, a 512 KiB JSON cap, a 2 MiB cover cap, a fixed User-Agent (`Shelf/0.1 (+https://github.com/inarun/Shelf)`), a same-host redirect cap of 3, and Content-Type validation (`application/json` for metadata; `image/jpeg`/`image/png` only for covers). ISBN values are normalized + digit-only-validated before URL interpolation; search queries are `url.QueryEscape`'d. No auth, no cookies, no telemetry.
- ~~**Inline style attributes under strict CSP:**~~ *Resolved 2026-04-17 (Session 7).* Discovered post-v0.1 that `style-src 'self'` (without `'unsafe-inline'`) blocks any `<element style="…">` parsed from HTML. Symptoms: stats bars rendered at 0-width; import form lost its flex layout. Fix is class-based: data-derived numeric widths go through a `barWidthClass(value, max int64) string` template helper emitting 5%-step utility classes (`bar--w0` through `bar--w100`), and static layout goes through named component classes (`.import-plan-form`, `.import-apply-row`). JS-set `.style.*` assignments are *not* blocked by `style-src` per the CSP spec (programmatic property access is permitted), but Session 8 converts those to class toggles for consistency. Regression is prevented by `TestNoInlineStyleAttributesInTemplates` in `internal/http/templates/templates_test.go`.

---

End of spec. This document is authoritative. When in doubt, re-read it.
