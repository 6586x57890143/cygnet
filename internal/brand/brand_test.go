package brand

import (
	"bytes"
	"image/png"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
)

// Every colour must resolve to an icon that is actually embedded, and the
// thumbnail and the rule must each name a file that is uploaded with them.
func TestEveryMoodHasItsFile(t *testing.T) {
	for color := range moods {
		e, files := Embed(color, "status", "t", "d")
		if len(files) != 2 {
			t.Fatalf("%#06x: %d files before any sync, want the rule and the icon", color, len(files))
		}
		for i, url := range []*discord.EmbedResource{e.Image, e.Thumbnail} {
			f := files[i]
			if url == nil || url.URL != "attachment://"+f.Name {
				t.Fatalf("%#06x: %v does not match file %q", color, url, f.Name)
			}
			b, _ := io.ReadAll(f.Reader)
			if len(b) == 0 {
				t.Fatalf("%#06x: %s is empty or missing", color, f.Name)
			}
		}
	}
	if e, f := Embed(0x123456, "status", "", ""); !strings.Contains(f[1].Name, "info") || e.Title != "" {
		t.Fatalf("unknown colour got %s, want the info icon", f[1].Name)
	}
	// Before the sync there's no icon to lead with: the title alone, as
	// written. The footer leads with the module, and notes follow it.
	e, _ := Embed(ColorOK, "status", "status", "")
	Note(&e, "since boot")
	if e.Title != "status" || e.Footer.Text != "status · since boot" {
		t.Fatalf("title %q, footer %q", e.Title, e.Footer.Text)
	}
	var bare discord.Embed
	if Note(&bare, "x"); bare.Footer.Text != "x" {
		t.Fatalf("footer %q", bare.Footer.Text)
	}
}

// grid is fields as rows of what each column holds, "_" for a blank and
// "=" before a field at full width.
func grid(fs []discord.EmbedField) string {
	var out []string
	row := ""
	n := 0
	for _, f := range fs {
		name := f.Name
		if name == zwsp {
			name = "_"
		}
		if !*f.Inline {
			out = append(out, "="+name)
			continue
		}
		row += name + " "
		if n++; n == columns {
			out = append(out, strings.TrimSpace(row))
			row, n = "", 0
		}
	}
	return strings.Join(out, " | ")
}

// The grid (DESIGN.md): every row is three columns so the columns line up,
// each section starts its own row, a section's fields are spread evenly
// over its rows, and a value too wide for a column, or on more than one
// line, goes under its section at full width. Keys stay lowercase.
func TestFieldsGrid(t *testing.T) {
	for _, c := range []struct {
		name     string
		sections [][][2]string
		want     string
	}{
		{"one", [][][2]string{{{"a", "1"}}}, "a _ _"},
		{"four as two and two", [][][2]string{{{"a", "1"}, {"b", "2"}, {"c", "3"}, {"d", "4"}}}, "a b _ | c d _"},
		{"five as three and two", [][][2]string{{{"a", "1"}, {"b", "2"}, {"c", "3"}, {"d", "4"}, {"e", "5"}}}, "a b c | d e _"},
		{"sections start rows", [][][2]string{{{"a", "1"}}, {{"b", "2"}, {"c", "3"}}}, "a _ _ | b c _"},
		{"wide under its section", [][][2]string{{{"a", "1"}, {"long", "guilds, members, message content"}, {"lines", "x\ny"}}, {{"b", "2"}}}, "a _ _ | =long | =lines | b _ _"},
		{"a mention is a name wide", [][][2]string{{{"by", "<@175928847299117063>"}, {"in", "[a long link name here](https://x.y)"}}}, "by _ _ | =in"},
		{"the actor is always a column", [][][2]string{{{"actor", "cygnet (automatic)"}}}, "actor _ _"},
	} {
		if got := grid(Fields(c.sections...)); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	many := make([][2]string, 40)
	for i := range many {
		many[i] = [2]string{"k", "v"}
	}
	if got := Fields(many); len(got) != maxFields {
		t.Errorf("%d fields, want Discord's %d", len(got), maxFields)
	}
}

// Every embedded icon is compiled into the binary and either uploaded with
// a reply or as an emoji, so its size is a budget, not a detail.
// tools/sprites writes them as indexed PNGs; a change that regresses to
// true colour fails here.
func TestEmbeddedAssetsStaySmall(t *testing.T) {
	const budget = 10 << 10
	for _, a := range art(t) {
		if len(a.data) >= budget {
			t.Errorf("%s is %d bytes, over the %d byte budget", a.path, len(a.data), budget)
		}
	}
}

// Every icon is on its grid (UX.md): the attachments are 256px of 2px
// cells, the bird's 128 cells scaled by 2. Emoji are 128px: a mood is the
// bird at 1px cells, a module tile 4px cells. The rule is 1040x32 of 4px
// cells. A cell that is not one flat colour is a resampling bug.
func TestEmbeddedAssetsAreOnTheirGrid(t *testing.T) {
	for _, a := range art(t) {
		img, err := png.Decode(bytes.NewReader(a.data))
		if err != nil {
			t.Fatalf("%s: %v", a.path, err)
		}
		w, h, k := 256, 256, 2
		switch {
		case a.path == "assets/cygnet_rule.png":
			w, h, k = 1040, 32, 4
		case strings.HasPrefix(a.path, "emoji/mod_"):
			w, h, k = 128, 128, 4
		case strings.HasPrefix(a.path, "emoji/"):
			w, h, k = 128, 128, 1
		}
		if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
			t.Errorf("%s is %dx%d, want %dx%d", a.path, b.Dx(), b.Dy(), w, h)
			continue
		}
	cells:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if img.At(x, y) != img.At(x-x%k, y-y%k) {
					t.Errorf("%s: pixel %d,%d breaks its %dpx cell", a.path, x, y, k)
					break cells
				}
			}
		}
	}
}

type file struct {
	path string
	data []byte
}

// art is every embedded PNG, attachments and emoji.
func art(t *testing.T) []file {
	t.Helper()
	var out []file
	for _, fsys := range []fs.FS{assets, emojiArt} {
		paths, err := fs.Glob(fsys, "*/*.png")
		if err != nil || len(paths) == 0 {
			t.Fatalf("no art: %v", err)
		}
		for _, p := range paths {
			data, err := fs.ReadFile(fsys, p)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, file{p, data})
		}
	}
	return out
}
