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
		{[]string{"a"}, "A _ _"},
		{[]string{"a", "b"}, "A B _"},
		{[]string{"a", "b", "c"}, "A B C"},
		{[]string{"a", "b", "c", "d"}, "A B _ | C D _"},
		{[]string{"a", "b", "c", "d", "e"}, "A B C | D E _"},
		{[]string{"a", "b", "c", "d", "e", "f", "g"}, "A B C | D E _ | F G _"},
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
	if strings.Join(names, ",") != strings.Join([]string{"A", zwsp, zwsp, "Long", "B", zwsp, zwsp}, ",") {
		t.Errorf("sections: %q", names)
	}
}
