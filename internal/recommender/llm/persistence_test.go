package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/inarun/Shelf/internal/recommender/rules"
)

func newTunedTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, recommenderSubdir), 0o700); err != nil {
		t.Fatalf("mkdir recommender: %v", err)
	}
	return dir
}

func sampleTuned() *TunedWeights {
	return &TunedWeights{
		SchemaVersion: TunedSchemaVersion,
		TunedAt:       time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		PromptVersion: "2026-04-v1",
		Model:         "claude-haiku-4-5",
		Weights: rules.Weights{
			SeriesCompletion: 1.3,
			AuthorAffinity:   1.1,
			ShelfSimilarity:  0.9,
			LengthMatch:      0.5,
			GenreMatch:       0.7,
			AxisMatch:        1.2,
		},
		AxisTargets: map[string]float64{
			"emotional_impact":              4.5,
			"characters":                    4.7,
			"plot":                          4.2,
			"dialogue_prose":                4.0,
			"cinematography_worldbuilding": 4.6,
		},
	}
}

func TestSave_Load_RoundTrip(t *testing.T) {
	dir := newTunedTestDir(t)
	want := sampleTuned()

	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("Load returned nil after Save")
	}
	if !reflect.DeepEqual(got.Weights, want.Weights) {
		t.Errorf("Weights: got %+v want %+v", got.Weights, want.Weights)
	}
	if !reflect.DeepEqual(got.AxisTargets, want.AxisTargets) {
		t.Errorf("AxisTargets: got %+v want %+v", got.AxisTargets, want.AxisTargets)
	}
	if got.SchemaVersion != want.SchemaVersion {
		t.Errorf("SchemaVersion: got %d want %d", got.SchemaVersion, want.SchemaVersion)
	}
	if !got.TunedAt.Equal(want.TunedAt) {
		t.Errorf("TunedAt: got %v want %v", got.TunedAt, want.TunedAt)
	}
	if got.PromptVersion != want.PromptVersion {
		t.Errorf("PromptVersion: got %q want %q", got.PromptVersion, want.PromptVersion)
	}
	if got.Model != want.Model {
		t.Errorf("Model: got %q want %q", got.Model, want.Model)
	}
}

func TestLoad_ReturnsNilWhenFileAbsent(t *testing.T) {
	dir := newTunedTestDir(t)
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != nil {
		t.Errorf("Load returned non-nil for absent file: %+v", got)
	}
}

func TestLoad_ReturnsErrorOnCorruptJSON(t *testing.T) {
	dir := newTunedTestDir(t)
	path := filepath.Join(dir, recommenderSubdir, TunedFilename)
	if err := os.WriteFile(path, []byte(`{not json`), 0o600); err != nil {
		t.Fatalf("seed corrupt: %v", err)
	}
	got, err := Load(dir)
	if err == nil {
		t.Fatal("want error for corrupt JSON")
	}
	if got != nil {
		t.Errorf("Load returned non-nil alongside error: %+v", got)
	}
}

func TestSave_AtomicReplace(t *testing.T) {
	dir := newTunedTestDir(t)
	first := sampleTuned()
	first.Model = "claude-haiku-4-5"
	if err := Save(dir, first); err != nil {
		t.Fatalf("Save #1: %v", err)
	}

	second := sampleTuned()
	second.Model = "claude-opus-4-7"
	if err := Save(dir, second); err != nil {
		t.Fatalf("Save #2: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Model != "claude-opus-4-7" {
		t.Errorf("Model=%q, want claude-opus-4-7 (second write)", got.Model)
	}
}

func TestSave_FilePerms0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file-mode bits are not honored by os.Chmod on Windows the same way")
	}
	dir := newTunedTestDir(t)
	if err := Save(dir, sampleTuned()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	path := filepath.Join(dir, recommenderSubdir, TunedFilename)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("perm=%o, want 0600", mode)
	}
}

func TestSave_NilReturnsError(t *testing.T) {
	dir := newTunedTestDir(t)
	if err := Save(dir, nil); err == nil {
		t.Error("Save(nil): want error")
	}
}

func TestSave_EmitsParseableJSON(t *testing.T) {
	// Regression guard — the on-disk format is human-readable (indented)
	// and json-parseable. Future sessions (and anyone debugging) should
	// be able to cat tuned.json and read it.
	dir := newTunedTestDir(t)
	if err := Save(dir, sampleTuned()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	path := filepath.Join(dir, recommenderSubdir, TunedFilename)
	// #nosec G304 -- test file, path is t.TempDir-rooted.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var scratch map[string]any
	if err := json.Unmarshal(data, &scratch); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if _, ok := scratch["schema_version"]; !ok {
		t.Error("missing schema_version key")
	}
	if _, ok := scratch["weights"]; !ok {
		t.Error("missing weights key")
	}
}
