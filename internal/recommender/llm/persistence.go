package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/inarun/Shelf/internal/recommender/rules"
	"github.com/inarun/Shelf/internal/vault/atomic"
)

// TunedSchemaVersion marks the on-disk tuned.json schema. Bump when
// the TunedWeights fields change in a way old readers can't handle.
// S23's Tune persists with this; S23's Load uses it to decide whether
// a stored artifact is consumable.
const TunedSchemaVersion = 1

// TunedFilename is the basename inside {data.directory}/recommender/.
const TunedFilename = "tuned.json"

// recommenderSubdir is the directory under data.directory where
// recommender artifacts live. cmd/shelf/main.go creates it at
// startup; this package reads/writes but does not mkdir.
const recommenderSubdir = "recommender"

// TunedWeights is the on-disk artifact the LLM tuner produces (S23).
// SchemaVersion + PromptVersion let future sessions invalidate stale
// artifacts cleanly; Model records which Claude ID produced the tune.
// Weights has the exact shape of rules.Weights so S23's ranker can
// consume it directly; AxisTargets carries per-axis preference means
// the tune may nudge.
type TunedWeights struct {
	SchemaVersion int                `json:"schema_version"`
	TunedAt       time.Time          `json:"tuned_at"`
	PromptVersion string             `json:"prompt_version"`
	Model         string             `json:"model"`
	Weights       rules.Weights      `json:"weights"`
	AxisTargets   map[string]float64 `json:"axis_targets"`
}

// Load reads {dataDir}/recommender/tuned.json and decodes it. Returns
// (nil, nil) when the file is absent — a fresh install has no tuned
// artifact and callers (S23's rankRecommendations) fall back to
// rules.DefaultWeights. Corrupt JSON or other read errors surface as
// errors; the caller decides whether to warn-and-fall-back or hard-fail.
func Load(dataDir string) (*TunedWeights, error) {
	path := filepath.Join(dataDir, recommenderSubdir, TunedFilename)
	// #nosec G304 -- path is built from the config-validated data.directory
	// plus constant subdir and filename; no user-supplied fragments.
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("llm: read %s: %w", path, err)
	}
	var tw TunedWeights
	if err := json.Unmarshal(data, &tw); err != nil {
		return nil, fmt.Errorf("llm: decode %s: %w", path, err)
	}
	return &tw, nil
}

// Save atomically writes tw to {dataDir}/recommender/tuned.json via
// internal/vault/atomic.Write. Caller is responsible for having
// created the recommender/ parent directory (cmd/shelf/main.go does
// this at startup alongside backups/ and covers/). Perm is 0o600 —
// the artifact can encode user rating patterns and is single-user.
func Save(dataDir string, tw *TunedWeights) error {
	if tw == nil {
		return errors.New("llm: nil TunedWeights")
	}
	data, err := json.MarshalIndent(tw, "", "  ")
	if err != nil {
		return fmt.Errorf("llm: encode tuned weights: %w", err)
	}
	path := filepath.Join(dataDir, recommenderSubdir, TunedFilename)
	if err := atomic.Write(path, data, 0o600); err != nil {
		return fmt.Errorf("llm: save tuned weights: %w", err)
	}
	return nil
}
