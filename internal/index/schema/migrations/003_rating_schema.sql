-- v0.2.1 Session 16 — migrate the `rating` column from a single scalar
-- to the Trial-System shape defined in v0.2.1.
--
-- Before: `rating INTEGER` held the rounded overall only.
-- After:
--   `rating_overall REAL`      — the floating-point overall (can exceed 5.0
--                                for "bumped" outliers like 6/5)
--   `rating_dimensions TEXT`   — JSON of per-axis map, empty string when
--                                the note has no dimensioned rating
--   `rating_has_override INTEGER` — 1 when frontmatter carries an explicit
--                                `overall:` value; 0 when the overall is
--                                derived from the trial_system mean
--
-- Legacy scalar ratings on disk migrate on the next sync (staleness pair
-- picks them up via mtime); this migration only copies the existing
-- rounded-integer DB value forward so the index reflects what's already
-- on disk until sync re-reads each note. Data migration is pure SQL —
-- no frontmatter parsing happens here.
--
-- `PRAGMA user_version = 3` closes the migration so the binary-downgrade
-- guard in schema.Migrate() can reject old binaries booting on a v3 DB.

ALTER TABLE books ADD COLUMN rating_overall REAL;
ALTER TABLE books ADD COLUMN rating_dimensions TEXT NOT NULL DEFAULT '';
ALTER TABLE books ADD COLUMN rating_has_override INTEGER NOT NULL DEFAULT 0;

UPDATE books
   SET rating_overall = CAST(rating AS REAL),
       rating_has_override = 1
 WHERE rating IS NOT NULL;

CREATE INDEX idx_books_rating_overall ON books(rating_overall);

ALTER TABLE books DROP COLUMN rating;

PRAGMA user_version = 3;
