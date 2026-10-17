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
		e, files := Embed(color, "t", "d")
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
	if _, f := Embed(0x123456, "", ""); !strings.Contains(f[1].Name, "info") {
		t.Fatalf("unknown colour got %s, want the info icon", f[1].Name)
	}
}

// A short value sits in a column and a long one takes the row, so three
// abreast never wrap into a ragged block.
func TestFieldsSetShortValuesAbreast(t *testing.T) {
	got := Fields([][2]string{{"gateway", "42 ms"}, {"intents", "guilds, members, message content"}})
	if len(got) != 2 || !*got[0].Inline || *got[1].Inline || got[0].Name != "gateway" || got[1].Value != "guilds, members, message content" {
		t.Fatalf("fields %+v", got)
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
