package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/inarun/Shelf/internal/covers"
	"github.com/inarun/Shelf/internal/http/middleware"
	syncpkg "github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/providers/metadata"
	"github.com/inarun/Shelf/internal/vault/body"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
	"github.com/inarun/Shelf/internal/vault/note"
	"github.com/inarun/Shelf/internal/vault/paths"
)

// openLibraryProviderName is the cache key namespace used for Open
// Library cover refs. Hardcoded here because Session 6 ships only one
// provider; a multi-provider world would resolve this from the Provider
// itself.
const openLibraryProviderName = "openlibrary"

// AddPageData is the template data for add.html.
type AddPageData struct {
	PageCommon
	ProviderWired bool
}

// AddPage renders /add. A nil MetadataProvider yields a page that
// disables the forms and explains why — this is a defensive fallback;
// cmd/shelf always wires the provider.
func (d *Dependencies) AddPage(w http.ResponseWriter, r *http.Request) {
	d.renderHTML(w, r, "add", AddPageData{
		PageCommon:    d.newPageCommon(r, "add"),
		ProviderWired: d.Metadata != nil && d.Covers != nil,
	})
}

// AddLookupRequest is the POST /api/add/lookup body.
type addLookupRequest struct {
	ISBN string `json:"isbn"`
}

// AddLookup handles ISBN lookup against the metadata provider. Returns
// the normalized Metadata object as JSON. 404 on not-found; 502 on
// provider transport failure so the frontend can distinguish.
func (d *Dependencies) AddLookup(w http.ResponseWriter, r *http.Request) {
	if d.Metadata == nil {
		d.writeJSONError(w, r, http.StatusServiceUnavailable, "server", "metadata provider not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
	var req addLookupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "invalid JSON body")
		return
	}
	isbn := normalizeISBN(req.ISBN)
	if !looksLikeISBN(isbn) {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "isbn must be 10 or 13 digits")
		return
	}

	m, err := d.Metadata.LookupByISBN(r.Context(), isbn)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			d.writeJSONError(w, r, http.StatusNotFound, "not_found", "no record for that ISBN")
			return
		}
		d.Logger.Warn("metadata lookup",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusBadGateway, "server", "metadata lookup failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"metadata": metadataJSON(m)})
}

// AddSearchRequest is the POST /api/add/search body.
type addSearchRequest struct {
	Q string `json:"q"`
}

// AddSearch handles free-text search against the metadata provider.
func (d *Dependencies) AddSearch(w http.ResponseWriter, r *http.Request) {
	if d.Metadata == nil {
		d.writeJSONError(w, r, http.StatusServiceUnavailable, "server", "metadata provider not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
	var req addSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "invalid JSON body")
		return
	}
	q := strings.TrimSpace(req.Q)
	if q == "" {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "q is required")
		return
	}
	if len(q) > 256 {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "q too long (max 256)")
		return
	}

	results, err := d.Metadata.Search(r.Context(), q)
	if err != nil {
		d.Logger.Warn("metadata search",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusBadGateway, "server", "search failed")
		return
	}

	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		out = append(out, searchResultJSON(r))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"results": out})
}

// AddCoverRequest is the POST /api/add/cover body.
type addCoverRequest struct {
	Ref string `json:"ref"`
}

// AddCover fetches (or hits the cache for) the cover identified by
// `ref`. The return value is a cover reference the caller can feed
// directly into an <img src="..."> attribute — always "/covers/<hash>.<ext>"
// on our origin, never the upstream URL. The download lives entirely
// server-side so the CSP (img-src 'self') stays intact.
func (d *Dependencies) AddCover(w http.ResponseWriter, r *http.Request) {
	if d.Metadata == nil || d.Covers == nil {
		d.writeJSONError(w, r, http.StatusServiceUnavailable, "server", "metadata or covers not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
	var req addCoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "invalid JSON body")
		return
	}
	ref := strings.TrimSpace(req.Ref)
	if ref == "" || len(ref) > 128 {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "ref required (<=128 chars)")
		return
	}
	coverRef, err := d.fetchAndCacheCover(r, ref)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			d.writeJSONError(w, r, http.StatusNotFound, "not_found", "no cover for that ref")
			return
		}
		d.writeJSONError(w, r, http.StatusBadGateway, "server", "cover fetch failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"cover": coverRef})
}

// fetchAndCacheCover is the shared path for both AddCover and the
// book-detail refresh endpoint: dedupes against the on-disk cache
// first, then calls the provider and stores the result. A cache hit
// skips the upstream fetch entirely — which matters for preview +
// submit (the client calls /api/add/cover during preview, then the
// /api/add/create path resolves the same ref again to confirm the file
// is ours). Provider errors and ErrNotFound are returned verbatim so
// the caller can map status codes.
func (d *Dependencies) fetchAndCacheCover(r *http.Request, ref string) (string, error) {
	providerKey := covers.ProviderKey(openLibraryProviderName, ref)

	if cached, ok := d.Covers.Find(providerKey); ok {
		return cached, nil
	}

	img, err := d.Metadata.FetchCover(r.Context(), ref)
	if err != nil {
		d.Logger.Warn("cover fetch",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"ref", ref,
			"err", err,
		)
		return "", err
	}
	coverRef, err := d.Covers.Store(providerKey, img)
	if err != nil {
		d.Logger.Error("cover store",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		return "", err
	}
	return coverRef, nil
}

// AddCreateRequest is the POST /api/add/create body. Only Title and at
// least one author are strictly required; every other field is
// optional.
type addCreateRequest struct {
	Title       string   `json:"title"`
	Subtitle    string   `json:"subtitle"`
	Authors     []string `json:"authors"`
	Publisher   string   `json:"publisher"`
	PublishDate string   `json:"publish_date"`
	TotalPages  *int     `json:"total_pages"`
	ISBN        string   `json:"isbn"`
	Categories  []string `json:"categories"`
	Cover       string   `json:"cover"`
	Series      string   `json:"series"`
	SeriesIndex *float64 `json:"series_index"`
	Format      string   `json:"format"`
	Source      string   `json:"source"`
}

// AddCreate handles POST /api/add/create. Writes a new book note
// derived from the metadata the client previewed. Canonical filename
// ({Title} by {Author}.md) is generated on the server; the client
// never dictates it.
func (d *Dependencies) AddCreate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
	var req addCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "invalid JSON body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "title is required")
		return
	}
	authors := trimAll(req.Authors)
	if len(authors) == 0 {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "at least one author is required")
		return
	}

	filename, err := paths.Generate(title, authors[0])
	if err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "cannot build filename: "+err.Error())
		return
	}
	abs, err := paths.ValidateWithinVault(d.BooksAbs, filename)
	if err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "path validation: "+err.Error())
		return
	}

	// Validate the optional fields up-front so a bad request never
	// touches disk.
	if req.ISBN != "" {
		if n := normalizeISBN(req.ISBN); !looksLikeISBN(n) {
			d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "isbn must be 10 or 13 digits")
			return
		}
	}
	if req.Format != "" {
		switch req.Format {
		case "audiobook", "ebook", "physical":
		default:
			d.writeJSONError(w, r, http.StatusBadRequest, "invalid",
				"format must be audiobook|ebook|physical (or empty)")
			return
		}
	}
	if req.Cover != "" {
		// Ignore any client-supplied cover that isn't one of our own
		// cache refs — prevents a malicious caller from planting an
		// external URL in the frontmatter.
		if !strings.HasPrefix(req.Cover, covers.CoverRefPrefix) || !d.Covers.Exists(req.Cover) {
			d.Logger.Warn("ignoring untrusted cover ref",
				"request_id", middleware.RequestIDFrom(r.Context()),
				"ref", req.Cover,
			)
			req.Cover = ""
		}
	}
	if req.TotalPages != nil && *req.TotalPages < 0 {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "total_pages must be non-negative")
		return
	}
	if req.SeriesIndex != nil && *req.SeriesIndex < 0 {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "series_index must be non-negative")
		return
	}

	// Build the frontmatter + body.
	fm := frontmatter.NewEmpty()
	fm.SetTag("📚Book")
	fm.SetTitle(title)
	if req.Subtitle != "" {
		fm.SetSubtitle(strings.TrimSpace(req.Subtitle))
	}
	fm.SetAuthors(authors)
	if req.Publisher != "" {
		fm.SetPublisher(strings.TrimSpace(req.Publisher))
	}
	if req.PublishDate != "" {
		fm.SetPublish(strings.TrimSpace(req.PublishDate))
	}
	if req.TotalPages != nil {
		if err := fm.SetTotalPages(req.TotalPages); err != nil {
			d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "total_pages: "+err.Error())
			return
		}
	}
	if req.ISBN != "" {
		fm.SetISBN(normalizeISBN(req.ISBN))
	}
	if len(req.Categories) > 0 {
		fm.SetCategories(trimAll(req.Categories))
	}
	if req.Cover != "" {
		fm.SetCover(req.Cover)
	}
	if req.Series != "" {
		fm.SetSeries(strings.TrimSpace(req.Series))
	}
	if req.SeriesIndex != nil {
		if err := fm.SetSeriesIndex(req.SeriesIndex); err != nil {
			d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "series_index: "+err.Error())
			return
		}
	}
	if req.Format != "" {
		if err := fm.SetFormat(req.Format); err != nil {
			d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "format: "+err.Error())
			return
		}
	}
	if req.Source != "" {
		fm.SetSource(strings.TrimSpace(req.Source))
	}
	if err := fm.SetStatus("unread"); err != nil {
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "status init: "+err.Error())
		return
	}
	fm.SetReadCount(0)

	b, err := body.Parse([]byte(""))
	if err != nil {
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "body init: "+err.Error())
		return
	}

	if err := note.Create(abs, fm, b); err != nil {
		if errors.Is(err, fs.ErrExist) {
			d.writeJSONError(w, r, http.StatusConflict, "stale",
				"a note with that filename already exists")
			return
		}
		d.Logger.Error("note create",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"filename", filename,
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "note create failed")
		return
	}

	// Reindex the new file so the library list picks it up immediately.
	if err := d.Syncer.Apply(r.Context(),
		syncpkg.Event{Kind: syncpkg.EventCreate, Path: abs}); err != nil {
		d.Logger.Warn("sync apply after add",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"filename", filename,
			"err", err,
		)
	}

	d.Logger.Info("add book",
		"request_id", middleware.RequestIDFrom(r.Context()),
		"filename", filename,
		"has_cover", req.Cover != "",
		"source", "openlibrary",
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":       true,
		"filename": filename,
		"url":      fmt.Sprintf("/books/%s", filename),
	})
}

// RefreshCoverBody for POST /api/books/{filename}/cover.
type refreshCoverRequest struct {
	Ref string `json:"ref"` // optional explicit ref; falls back to book's ISBN
}

// RefreshCover is POST /api/books/{filename}/cover. Re-fetches the
// cover for an existing book and updates its frontmatter.cover field.
// The ref comes from the client if provided, else from the book's ISBN
// column (isbn:<ISBN>). If neither is available, returns 400.
func (d *Dependencies) RefreshCover(w http.ResponseWriter, r *http.Request) {
	if d.Metadata == nil || d.Covers == nil {
		d.writeJSONError(w, r, http.StatusServiceUnavailable, "server", "metadata or covers not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
	var req refreshCoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "invalid JSON body")
		return
	}

	raw := r.PathValue("filename")
	abs, base, err := DecodeAndValidateFilename(d.BooksAbs, raw)
	if err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "filename: "+err.Error())
		return
	}

	n, err := note.Read(abs)
	if err != nil {
		d.writeJSONError(w, r, http.StatusNotFound, "not_found", "could not read note")
		return
	}

	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		if isbn := n.Frontmatter.ISBN(); looksLikeISBN(normalizeISBN(isbn)) {
			ref = "isbn:" + normalizeISBN(isbn)
		}
	}
	if ref == "" {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid",
			"no cover ref supplied and note has no valid ISBN")
		return
	}

	coverRef, err := d.fetchAndCacheCover(r, ref)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			d.writeJSONError(w, r, http.StatusNotFound, "not_found", "no cover for that ref")
			return
		}
		d.writeJSONError(w, r, http.StatusBadGateway, "server", "cover fetch failed")
		return
	}

	n.Frontmatter.SetCover(coverRef)
	if err := n.SaveFrontmatter(); err != nil {
		d.Logger.Error("refresh cover save",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"filename", base,
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "save frontmatter failed")
		return
	}
	if err := d.Syncer.Apply(r.Context(),
		syncpkg.Event{Kind: syncpkg.EventWrite, Path: abs}); err != nil {
		d.Logger.Warn("sync after cover refresh",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"filename", base,
			"err", err,
		)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"cover": coverRef})
}

// normalizeISBN strips whitespace, hyphens, and uppercases the 'X'
// check digit. Not a validator — just a canonicalizer.
func normalizeISBN(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '-' || c == '\t' {
			continue
		}
		if c == 'x' {
			c = 'X'
		}
		out = append(out, c)
	}
	return string(out)
}

// looksLikeISBN reports whether s has the shape of an ISBN-10 or
// ISBN-13 (digits only, with the final check character allowed to be
// 'X' for ISBN-10). Not a checksum validator — the transport only needs
// syntactic plausibility before interpolation.
func looksLikeISBN(s string) bool {
	if len(s) != 10 && len(s) != 13 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			continue
		}
		if len(s) == 10 && i == 9 && c == 'X' {
			continue
		}
		return false
	}
	return true
}

// trimAll returns a slice with each string trimmed and empty entries
// dropped.
func trimAll(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, 0, len(s))
	for _, v := range s {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// metadataJSON projects a metadata.Metadata into the UI-facing JSON
// shape.
func metadataJSON(m *metadata.Metadata) map[string]any {
	return map[string]any{
		"title":        m.Title,
		"subtitle":     m.Subtitle,
		"authors":      orEmpty(m.Authors),
		"publisher":    m.Publisher,
		"publish_date": m.PublishDate,
		"total_pages":  m.TotalPages,
		"isbn_10":      m.ISBN10,
		"isbn_13":      m.ISBN13,
		"categories":   orEmpty(m.Categories),
		"cover_ref":    m.CoverRef,
		"source_name":  m.SourceName,
		"source_id":    m.SourceID,
	}
}

// searchResultJSON projects a metadata.SearchResult.
func searchResultJSON(r metadata.SearchResult) map[string]any {
	return map[string]any{
		"title":        r.Title,
		"authors":      orEmpty(r.Authors),
		"publish_year": r.PublishYear,
		"isbn":         r.ISBN,
		"cover_ref":    r.CoverRef,
		"source_id":    r.SourceID,
	}
}

