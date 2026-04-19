package precedence

import "testing"

func TestResolve_VaultWinsWhenPopulated(t *testing.T) {
	got, ok := Resolve([]Candidate{
		{Source: SourceGoodreads, Value: []string{"Adams"}},
		{Source: SourceVaultFrontmatter, Value: []string{"Herbert"}},
		{Source: SourceMetadata, Value: []string{"Tolkien"}},
	})
	if !ok {
		t.Fatalf("Resolve: ok=false, want true")
	}
	if got.Source != SourceVaultFrontmatter {
		t.Errorf("Source=%s, want vault_frontmatter", got.Source)
	}
	authors, _ := got.Value.([]string)
	if len(authors) != 1 || authors[0] != "Herbert" {
		t.Errorf("Value=%v, want [Herbert]", got.Value)
	}
}

func TestResolve_ExternalWinsWhenVaultGap(t *testing.T) {
	got, ok := Resolve([]Candidate{
		{Source: SourceVaultFrontmatter, Value: []string{}},
		{Source: SourceGoodreads, Value: []string{"Herbert"}},
	})
	if !ok {
		t.Fatalf("Resolve: ok=false, want true")
	}
	if got.Source != SourceGoodreads {
		t.Errorf("Source=%s, want goodreads", got.Source)
	}
}

func TestResolve_AllGapsReturnsFalse(t *testing.T) {
	got, ok := Resolve([]Candidate{
		{Source: SourceVaultFrontmatter, Value: ""},
		{Source: SourceGoodreads, Value: []string(nil)},
		{Source: SourceMetadata, Value: nil},
	})
	if ok {
		t.Fatalf("Resolve: ok=true, want false (got=%+v)", got)
	}
}

func TestResolve_GoodreadsBeatsMetadata(t *testing.T) {
	got, ok := Resolve([]Candidate{
		{Source: SourceMetadata, Value: "meta"},
		{Source: SourceGoodreads, Value: "gr"},
	})
	if !ok || got.Source != SourceGoodreads {
		t.Errorf("Source=%s, want goodreads", got.Source)
	}
}

func TestResolve_AudiobookshelfBeatsKavita(t *testing.T) {
	got, ok := Resolve([]Candidate{
		{Source: SourceKavita, Value: "k"},
		{Source: SourceAudiobookshelf, Value: "ab"},
	})
	if !ok || got.Source != SourceAudiobookshelf {
		t.Errorf("Source=%s, want audiobookshelf", got.Source)
	}
}

func TestResolveWith_StatusUnreadIsGap(t *testing.T) {
	got, ok := ResolveWith([]Candidate{
		{Source: SourceVaultFrontmatter, Value: "unread"},
		{Source: SourceGoodreads, Value: "finished"},
	}, IsStatusGap)
	if !ok {
		t.Fatalf("ResolveWith: ok=false, want true")
	}
	if got.Source != SourceGoodreads {
		t.Errorf("Source=%s, want goodreads (unread must be gap)", got.Source)
	}
	if got.Value != "finished" {
		t.Errorf("Value=%v, want finished", got.Value)
	}
}

func TestResolveWith_StatusNonUnreadPopulatedVaultWins(t *testing.T) {
	got, ok := ResolveWith([]Candidate{
		{Source: SourceVaultFrontmatter, Value: "paused"},
		{Source: SourceGoodreads, Value: "finished"},
	}, IsStatusGap)
	if !ok || got.Source != SourceVaultFrontmatter {
		t.Errorf("Source=%s, want vault_frontmatter (non-unread wins)", got.Source)
	}
}

func TestIsGap_Table(t *testing.T) {
	intZero := 0
	strEmpty := ""
	var intNilPtr *int
	populated := "value"
	cases := []struct {
		name string
		v    any
		want bool
	}{
		{"nil", nil, true},
		{"empty string", "", true},
		{"non-empty string", "x", false},
		{"nil string slice", []string(nil), true},
		{"empty string slice", []string{}, true},
		{"populated string slice", []string{"x"}, false},
		{"nil int slice", []int(nil), true},
		{"nil map", map[string]int(nil), true},
		{"empty map", map[string]int{}, true},
		{"nil pointer", (*int)(nil), true},
		{"nil *int variable", intNilPtr, true},
		{"pointer to zero int", &intZero, false}, // populated: caller sees zero
		{"pointer to empty string", &strEmpty, false},
		{"pointer to populated string", &populated, false},
		{"zero int", 0, false}, // scalar zero is populated; use *int to signal absence
		{"non-zero int", 42, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsGap(tc.v); got != tc.want {
				t.Errorf("IsGap(%v) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

func TestIsStatusGap(t *testing.T) {
	cases := []struct {
		v    any
		want bool
	}{
		{"", true},
		{"unread", true},
		{"reading", false},
		{"paused", false},
		{"finished", false},
		{"dnf", false},
		{nil, true},
		{[]string(nil), true},  // delegates to IsGap
		{42, false},            // non-string, non-gap
	}
	for _, tc := range cases {
		if got := IsStatusGap(tc.v); got != tc.want {
			t.Errorf("IsStatusGap(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestPriority_Ordering(t *testing.T) {
	order := []Source{
		SourceVaultFrontmatter,
		SourceVaultBody,
		SourceGoodreads,
		SourceAudiobookshelf,
		SourceKavita,
		SourceMetadata,
		SourceUnknown,
	}
	for i := 0; i < len(order)-1; i++ {
		if Priority(order[i]) <= Priority(order[i+1]) {
			t.Errorf("Priority(%s)=%d not > Priority(%s)=%d",
				order[i], Priority(order[i]),
				order[i+1], Priority(order[i+1]))
		}
	}
}

func TestSource_String(t *testing.T) {
	cases := map[Source]string{
		SourceVaultFrontmatter: "vault_frontmatter",
		SourceVaultBody:        "vault_body",
		SourceGoodreads:        "goodreads",
		SourceAudiobookshelf:   "audiobookshelf",
		SourceKavita:           "kavita",
		SourceMetadata:         "metadata",
		SourceUnknown:          "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Source(%d).String() = %q, want %q", int(s), got, want)
		}
	}
}
