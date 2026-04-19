-- v0.1.2 Session 11 — add the missing sibling index on book_categories.
-- The author-side join already had idx_book_authors_author_id (001); the
-- category-side table shipped without it. No query filters by category_id
-- today, so current impact is zero, but closing the symmetry keeps
-- category-based filters (v0.3 recommender) from hitting a full table
-- scan later.

CREATE INDEX IF NOT EXISTS idx_book_categories_category_id
    ON book_categories(category_id);
