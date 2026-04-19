package timeline

import (
	"testing"
	"time"

	"github.com/inarun/Shelf/internal/domain/precedence"
)

// mustParseDay is a readability helper for test cases.
func mustParseDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return d
}

func TestMerge_Empty(t *testing.T) {
	if got := Merge(nil, nil); len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

func TestMerge_ExternalFillsGap(t *testing.T) {
	day1 := mustParseDay(t, "2026-01-10")
	day2 := mustParseDay(t, "2026-02-15")

	vault := []Entry{
		{Source: precedence.SourceVaultFrontmatter, Start: day1, End: day1, Kind: KindFinished},
	}
	external := []Entry{
		{ExternalID: "abs-1", Source: precedence.SourceAudiobookshelf, Start: day2, End: day2, Kind: KindFinished},
	}
	got := Merge(vault, external)
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	if !got[0].Start.Equal(day1) || got[0].Source != precedence.SourceVaultFrontmatter {
		t.Errorf("entry 0: want vault on %s, got %+v", day1, got[0])
	}
	if !got[1].Start.Equal(day2) || got[1].Source != precedence.SourceAudiobookshelf {
		t.Errorf("entry 1: want audiobookshelf on %s, got %+v", day2, got[1])
	}
}

func TestMerge_VaultWinsOnOverlap(t *testing.T) {
	start := mustParseDay(t, "2026-01-10")
	end := mustParseDay(t, "2026-01-20")

	vault := []Entry{
		{Source: precedence.SourceVaultFrontmatter, Start: start, End: end, Kind: KindFinished},
	}
	external := []Entry{
		// Exact overlap — dropped.
		{ExternalID: "abs-1", Source: precedence.SourceAudiobookshelf, Start: start, End: end, Kind: KindFinished},
		// Partial overlap — also dropped.
		{ExternalID: "abs-2", Source: precedence.SourceAudiobookshelf, Start: mustParseDay(t, "2026-01-15"), End: mustParseDay(t, "2026-01-25"), Kind: KindFinished},
	}
	got := Merge(vault, external)
	if len(got) != 1 {
		t.Fatalf("want 1 entry (vault kept, externals dropped), got %d: %+v", len(got), got)
	}
	if got[0].Source != precedence.SourceVaultFrontmatter {
		t.Errorf("want vault winner, got %s", got[0].Source)
	}
}

func TestMerge_DedupByExternalID(t *testing.T) {
	day := mustParseDay(t, "2026-03-01")
	external := []Entry{
		{ExternalID: "abs-1", Source: precedence.SourceAudiobookshelf, Start: day, End: day, Kind: KindFinished, Note: "first"},
		{ExternalID: "abs-1", Source: precedence.SourceAudiobookshelf, Start: day, End: day, Kind: KindFinished, Note: "second"},
	}
	got := Merge(nil, external)
	if len(got) != 1 {
		t.Fatalf("want 1 entry (deduped by ExternalID), got %d", len(got))
	}
	if got[0].Note != "first" {
		t.Errorf("want first-wins dedup, got note=%q", got[0].Note)
	}
}

func TestMerge_DedupBySourceDay(t *testing.T) {
	day := mustParseDay(t, "2026-03-01")
	// Two entries without ExternalID on the same day from the same source
	// collapse to the first.
	vault := []Entry{
		{Source: precedence.SourceVaultBody, Start: day, End: day, Kind: KindProgress, Note: "first"},
		{Source: precedence.SourceVaultBody, Start: day, End: day, Kind: KindProgress, Note: "second"},
	}
	got := Merge(vault, nil)
	if len(got) != 1 {
		t.Fatalf("want 1 entry (deduped by (Source, Date)), got %d", len(got))
	}
	if got[0].Note != "first" {
		t.Errorf("want first-wins dedup, got note=%q", got[0].Note)
	}
}

func TestMerge_OrderingByStartThenPriority(t *testing.T) {
	day1 := mustParseDay(t, "2026-01-01")
	day2 := mustParseDay(t, "2026-02-01")

	// Two same-day entries from different sources should sort by Source
	// priority descending after the Start tie.
	in := []Entry{
		{ExternalID: "abs-b", Source: precedence.SourceAudiobookshelf, Start: day2, End: day2, Kind: KindFinished},
		{Source: precedence.SourceVaultFrontmatter, Start: day1, End: day1, Kind: KindFinished},
		// Same start as the vault entry but lower priority — goes second.
		{ExternalID: "abs-a", Source: precedence.SourceAudiobookshelf, Start: day1, End: day1, Kind: KindProgress},
	}
	// Pass everything as external so dedup rule 2 doesn't collapse the
	// same-day pair (different Source ⇒ different (Source, Date) key).
	got := Merge(nil, in)
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got))
	}
	if got[0].Start != day1 || got[0].Source != precedence.SourceVaultFrontmatter {
		t.Errorf("entry 0: want vault on day1, got %+v", got[0])
	}
	if got[1].Start != day1 || got[1].Source != precedence.SourceAudiobookshelf {
		t.Errorf("entry 1: want audiobookshelf on day1 (lower priority goes second), got %+v", got[1])
	}
	if !got[2].Start.Equal(day2) {
		t.Errorf("entry 2: want day2, got %+v", got[2])
	}
}

func TestMerge_OngoingEntryTreatedAsPointInTime(t *testing.T) {
	// An external "reading, still ongoing" entry (End == zero) only
	// overlaps vault ranges that *contain* its Start. A future vault
	// finished-range must not absorb it.
	extStart := mustParseDay(t, "2026-01-10")
	vaultLater := mustParseDay(t, "2026-02-01")
	vaultLaterEnd := mustParseDay(t, "2026-02-10")

	vault := []Entry{
		{Source: precedence.SourceVaultFrontmatter, Start: vaultLater, End: vaultLaterEnd, Kind: KindFinished},
	}
	external := []Entry{
		{ExternalID: "abs-ongoing", Source: precedence.SourceAudiobookshelf, Start: extStart, Kind: KindProgress},
	}
	got := Merge(vault, external)
	if len(got) != 2 {
		t.Fatalf("want 2 entries (ongoing external kept + vault), got %d: %+v", len(got), got)
	}
	// Ordering: extStart (Jan 10) before vaultLater (Feb 1).
	if got[0].ExternalID != "abs-ongoing" {
		t.Errorf("want ongoing entry first by Start, got %+v", got[0])
	}
}

func TestMerge_OngoingEntryOverlapsContainingVaultRange(t *testing.T) {
	// If the vault range contains the ongoing entry's Start, the ongoing
	// entry is dropped (vault wins).
	vaultStart := mustParseDay(t, "2026-01-01")
	vaultEnd := mustParseDay(t, "2026-01-31")
	vault := []Entry{
		{Source: precedence.SourceVaultFrontmatter, Start: vaultStart, End: vaultEnd, Kind: KindFinished},
	}
	external := []Entry{
		// Ongoing, Start inside the vault range — dropped.
		{ExternalID: "abs-ongoing", Source: precedence.SourceAudiobookshelf, Start: mustParseDay(t, "2026-01-15"), Kind: KindProgress},
	}
	got := Merge(vault, external)
	if len(got) != 1 {
		t.Fatalf("want 1 entry (vault wins), got %d", len(got))
	}
	if got[0].Source != precedence.SourceVaultFrontmatter {
		t.Errorf("want vault winner, got %+v", got[0])
	}
}

func TestMerge_CrossSourceExternalIDCollision(t *testing.T) {
	// A vault entry and an external entry carry the same ExternalID
	// (rare, but possible once a sync round-trips into the vault).
	// Vault wins regardless of slice order.
	day := mustParseDay(t, "2026-05-01")
	vault := []Entry{
		{ExternalID: "abs-7", Source: precedence.SourceVaultBody, Start: day, End: day, Kind: KindFinished},
	}
	external := []Entry{
		{ExternalID: "abs-7", Source: precedence.SourceAudiobookshelf, Start: day, End: day, Kind: KindFinished, Note: "from external"},
	}
	got := Merge(vault, external)
	if len(got) != 1 {
		t.Fatalf("want 1 entry (deduped across sources by ExternalID), got %d", len(got))
	}
	if got[0].Source != precedence.SourceVaultBody {
		t.Errorf("want vault winner on cross-source ExternalID collision, got %+v", got[0])
	}
}
