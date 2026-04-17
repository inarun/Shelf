-- Session 2 initial schema. Keeps vault-is-truth semantics: every row
-- is reconstructible from the vault. No data lives here that isn't
-- derivable from a .md file's frontmatter + filename + stat.

CREATE TABLE series (
    id   INTEGER PRIMARY KEY,
    name TEXT    NOT NULL UNIQUE COLLATE NOCASE
);

CREATE TABLE authors (
    id   INTEGER PRIMARY KEY,
    name TEXT    NOT NULL UNIQUE COLLATE NOCASE
);

CREATE TABLE categories (
    id   INTEGER PRIMARY KEY,
    name TEXT    NOT NULL UNIQUE COLLATE NOCASE
);

CREATE TABLE books (
    id              INTEGER PRIMARY KEY,
    filename        TEXT    NOT NULL UNIQUE,
    canonical_name  INTEGER NOT NULL DEFAULT 1,
    title           TEXT    NOT NULL,
    subtitle        TEXT    NOT NULL DEFAULT '',
    publisher       TEXT    NOT NULL DEFAULT '',
    publish_date    TEXT    NOT NULL DEFAULT '',
    total_pages     INTEGER,
    isbn            TEXT    NOT NULL DEFAULT '',
    cover           TEXT    NOT NULL DEFAULT '',
    format          TEXT    NOT NULL DEFAULT '',
    source          TEXT    NOT NULL DEFAULT '',
    rating          INTEGER,
    status          TEXT    NOT NULL DEFAULT 'unread',
    read_count      INTEGER NOT NULL DEFAULT 0,
    series_id       INTEGER REFERENCES series(id),
    series_index    REAL,
    started_json    TEXT    NOT NULL DEFAULT '[]',
    finished_json   TEXT    NOT NULL DEFAULT '[]',
    size_bytes      INTEGER NOT NULL,
    mtime_ns        INTEGER NOT NULL,
    indexed_at_unix INTEGER NOT NULL,
    warnings_json   TEXT    NOT NULL DEFAULT '[]'
);

CREATE INDEX idx_books_title     ON books(title);
CREATE INDEX idx_books_status    ON books(status);
CREATE INDEX idx_books_series_id ON books(series_id);
CREATE INDEX idx_books_canonical ON books(canonical_name);

CREATE TABLE book_authors (
    book_id   INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    author_id INTEGER NOT NULL REFERENCES authors(id),
    position  INTEGER NOT NULL,
    PRIMARY KEY (book_id, position)
);

CREATE INDEX idx_book_authors_author_id ON book_authors(author_id);

CREATE TABLE book_categories (
    book_id     INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    category_id INTEGER NOT NULL REFERENCES categories(id),
    PRIMARY KEY (book_id, category_id)
);
