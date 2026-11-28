package reasoning

import (
	"slices"
	"testing"
)

func TestNormalizeAcceptsEveryStoredLevel(t *testing.T) {
	for _, level := range Levels {
		if got, err := Normalize(" " + level + " "); err != nil || got != level {
			t.Fatalf("Normalize(%q) = %q, %v", level, got, err)
		}
	}
	if got, err := Normalize(""); err != nil || got != "" {
		t.Fatalf("Normalize(\"\") = %q, %v", got, err)
	}
	if _, err := Normalize("turbo"); err == nil {
		t.Fatal("a level outside the vocabulary must be refused")
	}
}

// Every layer derives its own list from Levels, so a level an agent offers but
// the vocabulary lacks is a bug only a coverage test can catch.
func TestEndpointAndGradedAreInTheVocabulary(t *testing.T) {
	for _, level := range Endpoint {
		if !slices.Contains(Levels, level) {
			t.Fatalf("endpoint level %q is not in Levels", level)
		}
	}
	for _, level := range Graded {
		if !slices.Contains(Endpoint, level) {
			t.Fatalf("graded level %q is not an endpoint level", level)
		}
	}
}

func TestSortOrdersLevelsLowestFirst(t *testing.T) {
	levels := []string{Max, None, Minimal, XHigh, Off, High, Medium, Low}
	Sort(levels)
	if want := []string{Off, None, Minimal, Low, Medium, High, XHigh, Max}; !slices.Equal(levels, want) {
		t.Fatalf("Sort = %v, want %v", levels, want)
	}
}
