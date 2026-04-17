package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const importCSV = `Book Id,Title,Author,ISBN,ISBN13,My Rating,Exclusive Shelf,Date Read,Date Added,My Review,Publisher,Year Published,Number of Pages,Bookshelves
1,"Hyperion","Dan Simmons","=""0553283685""","=""9780553283686""",5,read,2025/04/02,2024/12/15,"Loved the structure.","Bantam",1989,482,"sci-fi"
2,"Project Hail Mary","Andy Weir","","=""9780593135204""",0,currently-reading,,2025/01/05,,"Ballantine",2021,476,"sci-fi"
`

func newMultipartPlanRequest(t *testing.T, url, csvText string) *http.Request {
	t.Helper()
	return newMultipartRequest(t, url, csvText, "")
}

func newMultipartApplyRequest(t *testing.T, url, csvText, decisions string) *http.Request {
	t.Helper()
	return newMultipartRequest(t, url, csvText, decisions)
}

func newMultipartRequest(t *testing.T, url, csvText, decisions string) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, err := w.CreateFormFile("csv", "export.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(csvText)); err != nil {
		t.Fatal(err)
	}
	if decisions != "" {
		if err := w.WriteField("decisions", decisions); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, url, body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestPlanImport_ReturnsPlanJSON(t *testing.T) {
	d, _ := seedDeps(t)
	rec := httptest.NewRecorder()
	d.PlanImport(rec, newMultipartPlanRequest(t, "/api/import/plan", importCSV))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	// Plan JSON should include required keys and stable empty slices.
	body := rec.Body.String()
	for _, want := range []string{`"will_create"`, `"will_update"`, `"will_skip"`, `"conflicts"`} {
		if !strings.Contains(body, want) {
			t.Errorf("plan body missing %q; got:\n%s", want, body)
		}
	}
}

func TestPlanImport_RejectsMissingFile(t *testing.T) {
	d, _ := seedDeps(t)
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	_ = w.WriteField("not_csv", "x")
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/import/plan", body)
	req.Header.Set("Content-Type", w.FormDataContentType())

	rec := httptest.NewRecorder()
	d.PlanImport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing csv: status = %d, want 400", rec.Code)
	}
}

func TestPlanImport_RejectsOversizedBody(t *testing.T) {
	d, _ := seedDeps(t)
	// Craft a request whose Content-Length exceeds the cap. We don't
	// really need 16 MiB of data — just trip the MaxBytesReader. Use a
	// stream that keeps reading past the cap.
	oversized := strings.Repeat("x", MaxCSVBytes+1024)
	req := newMultipartPlanRequest(t, "/api/import/plan", oversized)

	rec := httptest.NewRecorder()
	d.PlanImport(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusBadRequest {
		t.Errorf("oversized body: status = %d, want 413 or 400", rec.Code)
	}
}

func TestApplyImport_CreatesBooksAndBackup(t *testing.T) {
	d, books := seedDeps(t)
	rec := httptest.NewRecorder()
	d.ApplyImport(rec, newMultipartApplyRequest(t, "/api/import/apply", importCSV, "[]"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var report struct {
		BackupRoot string   `json:"backup_root"`
		Created    []string `json:"created"`
		Updated    []string `json:"updated"`
		Skipped    []string `json:"skipped"`
		Errors     []any    `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("parse report: %v body=%s", err, rec.Body.String())
	}
	if report.BackupRoot == "" {
		t.Error("backup_root empty — pre-apply snapshot should have happened")
	}
	// Backups root folder should exist on disk.
	if st, err := os.Stat(report.BackupRoot); err != nil || !st.IsDir() {
		t.Errorf("backup dir missing at %q: %v", report.BackupRoot, err)
	}
	// Hyperion existed; Project Hail Mary should be created.
	phm := filepath.Join(books, "Project Hail Mary by Andy Weir.md")
	if _, err := os.Stat(phm); err != nil {
		t.Errorf("Project Hail Mary not created: %v", err)
	}
	if len(report.Created) == 0 {
		t.Errorf("expected at least 1 created; got %+v", report)
	}
}

func TestApplyImport_RejectsInvalidDecisions(t *testing.T) {
	d, _ := seedDeps(t)
	req := newMultipartApplyRequest(t, "/api/import/apply", importCSV,
		`[{"filename":"x","action":"bogus"}]`)
	rec := httptest.NewRecorder()
	d.ApplyImport(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestParseDecisionsEmpty(t *testing.T) {
	got, err := parseDecisions("")
	if err != nil {
		t.Fatalf("empty should be nil: %v", err)
	}
	if got != nil {
		t.Errorf("want nil, got %v", got)
	}
}

func TestParseDecisionsValid(t *testing.T) {
	got, err := parseDecisions(`[{"filename":"a.md","action":"accept"},{"filename":"b.md","action":"skip"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Action != "accept" || got[1].Action != "skip" {
		t.Errorf("unexpected decisions: %+v", got)
	}
}
