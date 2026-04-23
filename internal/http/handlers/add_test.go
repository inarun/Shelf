package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAddCreate_ManualMinimalPayload_WritesNote exercises the minimal
// viable manual-entry payload — title + a single author — that the
// v0.3.3 S23 UI can produce. No metadata provider is wired; seedDeps
// omits Metadata and Covers. The handler must accept this shape, write
// a canonical note to disk, and respond with the expected JSON envelope.
func TestAddCreate_ManualMinimalPayload_WritesNote(t *testing.T) {
	d, books := seedDeps(t)

	body := strings.NewReader(`{"title":"Test Book","authors":["Test Author"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/add/create", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.AddCreate(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK       bool   `json:"ok"`
		Filename string `json:"filename"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}
	if !resp.OK {
		t.Errorf("ok = false")
	}
	if resp.Filename != "Test Book by Test Author.md" {
		t.Errorf("filename = %q, want %q", resp.Filename, "Test Book by Test Author.md")
	}
	if resp.URL != "/books/Test Book by Test Author.md" {
		t.Errorf("url = %q", resp.URL)
	}

	// Disk assertions: file exists; frontmatter carries the expected
	// minimal shape.
	noteBytes, err := os.ReadFile(filepath.Join(books, resp.Filename))
	if err != nil {
		t.Fatalf("read new note: %v", err)
	}
	note := string(noteBytes)
	for _, want := range []string{
		"title: Test Book",
		"Test Author",
		"status: unread",
		"read_count: 0",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q; content:\n%s", want, note)
		}
	}
	// Empty cover shouldn't leak an external URL. Either the field is
	// absent or it's an empty literal — both are acceptable.
	if strings.Contains(note, "cover: http") || strings.Contains(note, "cover: /covers/") {
		t.Errorf("manual payload produced a non-empty cover field; content:\n%s", note)
	}
}

// TestAddCreate_ManualFullPayload_WritesAllFields asserts every
// optional field supplied by a manual-entry form survives the YAML
// round-trip onto disk. The shape mirrors what initAddManual posts.
func TestAddCreate_ManualFullPayload_WritesAllFields(t *testing.T) {
	d, books := seedDeps(t)

	payload := map[string]any{
		"title":        "Full Manual Book",
		"subtitle":     "A Subtitle",
		"authors":      []string{"Alice Writer", "Bob Author"},
		"isbn":         "9780553293357",
		"format":       "physical",
		"publisher":    "Test Press",
		"publish_date": "2024-07-01",
		"total_pages":  321,
		"series":       "Test Saga",
		"series_index": 2.5,
		"categories":   []string{"Science fiction", "Adventure"},
		"cover":        "",
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/add/create", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.AddCreate(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Filename picks the first author.
	if resp.Filename != "Full Manual Book by Alice Writer.md" {
		t.Errorf("filename = %q", resp.Filename)
	}

	noteBytes, err := os.ReadFile(filepath.Join(books, resp.Filename))
	if err != nil {
		t.Fatalf("read new note: %v", err)
	}
	note := string(noteBytes)
	// Plain substring checks — every value we wrote must survive to disk.
	// YAML quoting style (quoted vs unquoted) is encoder-dependent, so
	// assert on the value verbatim rather than a quoted form.
	for _, want := range []string{
		"Full Manual Book",
		"A Subtitle",
		"Alice Writer",
		"Bob Author",
		"9780553293357",
		"physical",
		"Test Press",
		"2024-07-01",
		"321",
		"Test Saga",
		"2.5",
		"Science fiction",
		"Adventure",
		"unread",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing value %q; content:\n%s", want, note)
		}
	}
	// Sanity: the canonical keys landed on disk (not just the values).
	for _, key := range []string{
		"title:", "subtitle:", "authors:", "categories:",
		"publisher:", "publish:", "total_pages:", "isbn:",
		"series:", "series_index:", "format:", "status:",
	} {
		if !strings.Contains(note, key) {
			t.Errorf("note missing key %q; content:\n%s", key, note)
		}
	}
}

// TestAddCreate_ManualPayload_TriggersReindex asserts that after the
// manual POST succeeds, the SQLite index is updated via Syncer.Apply so
// the /library list picks the book up without a restart.
func TestAddCreate_ManualPayload_TriggersReindex(t *testing.T) {
	d, _ := seedDeps(t)

	body := strings.NewReader(`{"title":"Reindex Probe","authors":["Reindex Author"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/add/create", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.AddCreate(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	// LibraryView reads from the index; if reindex didn't happen, the
	// new book won't appear.
	rec2 := httptest.NewRecorder()
	d.LibraryView(rec2, httptest.NewRequest(http.MethodGet, "/library", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("library status = %d; body=%s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), "Reindex Probe") {
		t.Errorf("library did not include new book after manual POST; body:\n%s", rec2.Body.String())
	}
}
