package ledger

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWrapFitsAndKeepsEverything(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"short", []string{"short"}},
		{"a, b, c", []string{"a, b, c"}},
		// Items stay whole while one fits a line: a list reads by its items.
		{"booster, a role above the bot, one more", []string{"booster,", "a role above the bot,", "one more"}},
		{"alpha, beta, gamma, delta, epsilon, zeta", []string{"alpha, beta, gamma,", "delta, epsilon, zeta"}},
		// An item longer than a line goes by words, a word longer by cuts.
		{"one, an item far too long for a line", []string{"one, an item far too", "long for a line"}},
		{strings.Repeat("x", 30), []string{strings.Repeat("x", 24), "xxxxxx"}},
	} {
		got := wrap(tc.in, 25)
		if !slices.Equal(got, tc.want) {
			t.Errorf("wrap(%q) = %q, want %q", tc.in, got, tc.want)
		}
		for _, l := range got {
			if utf8.RuneCountInString(l) > 25 {
				t.Errorf("%q: line %q runs past the grid", tc.in, l)
			}
		}
		if strings.ReplaceAll(strings.Join(got, ""), " ", "") != strings.ReplaceAll(tc.in, " ", "") {
			t.Errorf("%q lost text: %q", tc.in, got)
		}
	}
}
