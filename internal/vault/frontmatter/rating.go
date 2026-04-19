package frontmatter

import (
	"fmt"
	"math"
	"strconv"

	"gopkg.in/yaml.v3"
)

// RatingAxes is the canonical order of trial-system rating dimensions.
// Serializers walk this slice to emit the YAML map in a stable order;
// validators reject any key not in this set.
var RatingAxes = []string{
	"emotional_impact",
	"characters",
	"plot",
	"dialogue_prose",
	"cinematography_worldbuilding",
}

// RatingAxisLabels maps each YAML axis key to the human-facing label used
// in the body `## Rating` block and the book-detail widget. Labels round-
// trip through body.parseRatingSection via a whitespace-insensitive lookup.
var RatingAxisLabels = map[string]string{
	"emotional_impact":             "Emotional Impact",
	"characters":                   "Characters",
	"plot":                         "Plot",
	"dialogue_prose":               "Dialogue/Prose",
	"cinematography_worldbuilding": "Cinematography/Worldbuilding",
}

// validAxisKey returns whether key is a recognized trial-system axis.
func validAxisKey(key string) bool {
	for _, a := range RatingAxes {
		if a == key {
			return true
		}
	}
	return false
}

// Rating is the structured five-axis "Trial System" rating introduced in
// v0.2.1. TrialSystem is a map keyed by RatingAxes entries; Overall is an
// optional override that wins over the computed mean. A Rating with an
// empty TrialSystem and nil Overall counts as absent (see IsEmpty).
//
// Legacy scalar ratings (pre-v0.2.1) deserialize into a Rating with a nil
// TrialSystem and Overall set to the scalar value — this gives the UI and
// precedence logic a single representation to consume.
type Rating struct {
	TrialSystem map[string]int
	Overall     *float64
}

// IsDimensioned reports whether the rating carries any per-axis values.
func (r *Rating) IsDimensioned() bool {
	return r != nil && len(r.TrialSystem) > 0
}

// HasOverride reports whether Overall is explicitly set (i.e., the user
// or importer supplied a value rather than leaving it computed).
func (r *Rating) HasOverride() bool {
	return r != nil && r.Overall != nil
}

// IsEmpty reports whether the rating carries neither axis values nor an
// override. Goodreads precedence treats empty ratings as gaps so external
// sources can populate them.
func (r *Rating) IsEmpty() bool {
	return !r.IsDimensioned() && !r.HasOverride()
}

// Effective returns the overall rating, either the explicit override or
// the mean of trial_system axis values. Returns 0 when the rating is
// empty — callers that care must guard with IsEmpty() first.
func (r *Rating) Effective() float64 {
	if r == nil {
		return 0
	}
	if r.Overall != nil {
		return *r.Overall
	}
	if len(r.TrialSystem) == 0 {
		return 0
	}
	sum := 0
	for _, v := range r.TrialSystem {
		sum += v
	}
	return float64(sum) / float64(len(r.TrialSystem))
}

// EffectiveRounded returns int(math.Round(Effective())) or nil when empty.
// Used by the index sync to populate the scalar `rating INTEGER` SQLite
// column until Session 16 adds a `rating_overall REAL` column.
func (r *Rating) EffectiveRounded() *int64 {
	if r == nil || r.IsEmpty() {
		return nil
	}
	n := int64(math.Round(r.Effective()))
	return &n
}

// Rating returns the structured rating, or nil when the field is absent
// or null. Accepts both the new map shape and the legacy scalar form:
// `rating: 4` deserializes into Rating{Overall: &4.0}. Unknown axis keys
// in the trial_system map are silently ignored on read (writes reject
// them via SetRating).
func (f *Frontmatter) Rating() *Rating {
	v := f.findValue(KeyRating)
	if v == nil || v.Tag == "!!null" {
		return nil
	}

	if v.Kind == yaml.ScalarNode {
		// Legacy scalar form: rating: 4.
		if v.Value == "" {
			return nil
		}
		n, err := strconv.ParseFloat(v.Value, 64)
		if err != nil {
			return nil
		}
		return &Rating{Overall: &n}
	}

	if v.Kind != yaml.MappingNode {
		return nil
	}

	out := &Rating{}
	for i := 0; i+1 < len(v.Content); i += 2 {
		key := v.Content[i].Value
		val := v.Content[i+1]
		switch key {
		case "trial_system":
			if val.Kind != yaml.MappingNode {
				continue
			}
			axes := map[string]int{}
			for j := 0; j+1 < len(val.Content); j += 2 {
				ak := val.Content[j].Value
				av := val.Content[j+1]
				if !validAxisKey(ak) {
					continue
				}
				if av.Kind != yaml.ScalarNode {
					continue
				}
				n, err := strconv.Atoi(av.Value)
				if err != nil {
					continue
				}
				axes[ak] = n
			}
			if len(axes) > 0 {
				out.TrialSystem = axes
			}
		case "overall":
			if val.Kind != yaml.ScalarNode || val.Value == "" || val.Tag == "!!null" {
				continue
			}
			n, err := strconv.ParseFloat(val.Value, 64)
			if err != nil {
				continue
			}
			out.Overall = &n
		}
	}
	if out.IsEmpty() {
		return nil
	}
	return out
}

// SetRating accepts nil to clear the field (emitted as a null scalar), or
// a non-empty Rating to serialize as a mapping node with `trial_system:`
// and optional `overall:` children. Unknown axis keys or negative axis
// values are rejected; Overall, when set, must be in 0..10.
func (f *Frontmatter) SetRating(r *Rating) error {
	if r == nil {
		f.setValue(KeyRating, nullScalar())
		return nil
	}
	if r.IsEmpty() {
		f.setValue(KeyRating, nullScalar())
		return nil
	}
	for k, v := range r.TrialSystem {
		if !validAxisKey(k) {
			return fmt.Errorf("rating: unknown axis %q", k)
		}
		if v < 0 {
			return fmt.Errorf("rating: axis %q value %d is negative", k, v)
		}
	}
	if r.Overall != nil {
		if *r.Overall < 0 || *r.Overall > 10 {
			return fmt.Errorf("rating: overall %v out of range 0..10", *r.Overall)
		}
	}

	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if r.IsDimensioned() {
		tsNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, axis := range RatingAxes {
			n, ok := r.TrialSystem[axis]
			if !ok {
				continue
			}
			tsNode.Content = append(tsNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: axis},
				scalarInt(n),
			)
		}
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "trial_system"},
			tsNode,
		)
	}
	if r.Overall != nil {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "overall"},
			scalarFloat(*r.Overall),
		)
	}
	f.setValue(KeyRating, mapping)
	return nil
}
