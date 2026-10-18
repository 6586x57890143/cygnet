package ledger

import (
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
)

// A section's columns are spread evenly over whole rows of three and none
// is dropped; a wide field follows its section.
func TestGridSpreadsEvenlyAndKeepsEverything(t *testing.T) {
	col := func(n string) discord.EmbedField { return field(n, "x") }
	for _, tc := range []struct {
		in   []string
		want string
	}{
		{[]string{"a"}, "a _ _"},
		{[]string{"a", "b"}, "a b _"},
		{[]string{"a", "b", "c"}, "a b c"},
		{[]string{"a", "b", "c", "d"}, "a b _ | c d _"},
		{[]string{"a", "b", "c", "d", "e"}, "a b c | d e _"},
		{[]string{"a", "b", "c", "d", "e", "f", "g"}, "a b c | d e _ | f g _"},
	} {
		var s []discord.EmbedField
		for _, n := range tc.in {
			s = append(s, col(n))
		}
		var rows []string
		var row []string
		for _, f := range grid(s) {
			name := f.Name
			if name == zwsp {
				name = "_"
			}
			row = append(row, name)
			if len(row) == columns {
				rows = append(rows, strings.Join(row, " "))
				row = nil
			}
		}
		if got := strings.Join(rows, " | "); got != tc.want || len(row) != 0 {
			t.Errorf("%d fields: got %q (+%v), want %q", len(tc.in), got, row, tc.want)
		}
	}
	wide := field("long", strings.Repeat("y", columnWidth+1))
	got := grid([]discord.EmbedField{col("a"), wide}, []discord.EmbedField{col("b")})
	var names []string
	for _, f := range got {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != strings.Join([]string{"a", zwsp, zwsp, "long", "b", zwsp, zwsp}, ",") {
		t.Errorf("sections: %q", names)
	}
}
