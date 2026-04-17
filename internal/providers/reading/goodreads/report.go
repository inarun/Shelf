package goodreads

import "encoding/json"

// MarshalJSON ensures empty slices serialize as "[]" rather than "null"
// so downstream UI code can safely `.length` / indexing without
// branching on null. Also, the JSON shape is frozen by SKILL.md
// §Goodreads — keep field ordering stable here.
func (p *Plan) MarshalJSON() ([]byte, error) {
	// Use local aliases with the same JSON tags but guaranteed non-nil
	// slices. A nil slice marshals as "null"; an empty slice marshals
	// as "[]". We want "[]".
	type jsonPlan struct {
		WillCreate []CreateEntry   `json:"will_create"`
		WillUpdate []UpdateEntry   `json:"will_update"`
		WillSkip   []SkipEntry     `json:"will_skip"`
		Conflicts  []ConflictEntry `json:"conflicts"`
	}
	out := jsonPlan{
		WillCreate: nonNilCreate(p.WillCreate),
		WillUpdate: nonNilUpdate(p.WillUpdate),
		WillSkip:   nonNilSkip(p.WillSkip),
		Conflicts:  nonNilConflict(p.Conflicts),
	}
	return json.Marshal(out)
}

func nonNilCreate(s []CreateEntry) []CreateEntry {
	if s == nil {
		return []CreateEntry{}
	}
	return s
}
func nonNilUpdate(s []UpdateEntry) []UpdateEntry {
	if s == nil {
		return []UpdateEntry{}
	}
	return s
}
func nonNilSkip(s []SkipEntry) []SkipEntry {
	if s == nil {
		return []SkipEntry{}
	}
	return s
}
func nonNilConflict(s []ConflictEntry) []ConflictEntry {
	if s == nil {
		return []ConflictEntry{}
	}
	return s
}
