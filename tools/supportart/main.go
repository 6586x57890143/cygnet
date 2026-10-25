// Command supportart draws the supporter's marks (docs/support.md): the
// badge, a heart held up by two feathered hands, with a gold frame for
// paying more; the plume for preview builds; the dawn for early access;
// and a lit bulb for a say in what gets built next.
// Each is an 11 by 11 glyph kept below as text, so a change to it is
// reviewable, painted at 8 px a pixel on a 32 cell tile of 4 px cells.
//
//	go run ./tools/supportart
//
// It writes edge/public/art/support for cygnet.lol, in two schemes: dusk,
// the mark, and paper, the same on a light page. Every bird that shows the
// badge copies these files, as the birds copy shared code. Edit here and
// rerun; never edit the PNGs.
//
// The tile is merlin's plugin icon tile in another palette: a frame with
// its corners cut, lit top left, its shaded side the frame at 60% so the
// border reads two cells wide on every side. The glyph's own ink is centred
// (12 px from the border on each side, its middle column on the centre
// line) and its shadow falls 4 px down and right of it, uncounted.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

// glyphs: 'w' is ink, 'a' the heart's red, 'b' amber.
var glyphs = map[string][]string{
	"badge": { // two hands of three feathers each, holding a heart up
		"...........",
		"...aa.aa...",
		"..aaaaaaa..",
		"..aaaaaaa..",
		"w..aaaaa..w",
		"ww..aaa..ww",
		"w.w..a..w.w",
		".w.w...w.w.",
		"..ww...ww..",
		"...w...w...",
		"...........",
	},
	"previews": { // a quill: preview builds
		"........www",
		"......wwwww",
		".....wwwwbw",
		"....wwwwbww",
		"...wwwwbww.",
		"...w.wbww..",
		"..wwwbww...",
		"..wwbww....",
		"..wbww.....",
		"..b........",
		"bb.........",
	},
	"say": { // a lit bulb, an idea: a say in what gets built next
		"....www....",
		"...wbbbw...",
		"..wbbbbbw..",
		"..wbbbbbw..",
		"..wbbbbbw..",
		"..wbbbbbw..",
		"...wbbbw...",
		"....wbw....",
		"....www....",
		"....www....",
		".....w.....",
	},
	// The coins and the chains a supporter can pay with, each in its own
	// colour so it reads at a glance, as the heart is red.
	"usdc": { // a blue coin cith a dollar sign
		"...uuuuu...",
		".uuuuuuuuu.",
		".uuuucuuuu.",
		"uuuuccccuuu",
		"uuucucuuuuu",
		"uuuucccuuuu",
		"uuuuucucuuu",
		"uuuccccuuuu",
		".uuuucuuuu.",
		".uuuuuuuuu.",
		"...uuuuu...",
	},
	"usdt": { // a green coin cith a T
		"...ttttt...",
		".ttttttttt.",
		".ttttttttt.",
		"ttccccccctt",
		"ttttccctttt",
		"ttttccctttt",
		"ttttccctttt",
		"ttttccctttt",
		".tttcccttt.",
		".ttttttttt.",
		"...ttttt...",
	},
	"sol": { // three slanted bars, violet to green
		"..vvvvvvvvv",
		".vvvvvvvvv.",
		"vvvvvvvvv..",
		"...........",
		"mmmmmmmmm..",
		".mmmmmmmmm.",
		"..mmmmmmmmm",
		"...........",
		"..ggggggggg",
		".ggggggggg.",
		"ggggggggg..",
	},
	"base": { // a blue disc cith a bar in from the left
		"...kkkkk...",
		".kkkkkkkkk.",
		".kkkkkkkkk.",
		"kkkkkkkkkkk",
		"kkkkkkkkkkk",
		"ccccccckkkk",
		"kkkkkkkkkkk",
		"kkkkkkkkkkk",
		".kkkkkkkkk.",
		".kkkkkkkkk.",
		"...kkkkk...",
	},
	"polygon": { // two linked hexagons
		".ppp.......",
		"p...p......",
		"p...pppp...",
		"p...p...p..",
		".pppp...p..",
		"..p.....p..",
		"..p...pppp.",
		"...pppp...p",
		"......p...p",
		"......p...p",
		".......ppp.",
	},
	"tron": { // a red triangle, lines drawn in it
		"rrrrrrrrrrr",
		"rr.......rr",
		"r.r.....r.r",
		"r..r...r..r",
		".r..r.r..r.",
		".r...r...r.",
		"..r..r..r..",
		"..r..r..r..",
		"...r.r.r...",
		"....rrr....",
		".....r.....",
	},
	"early": { // the sun on the horizon: early access
		".....b.....",
		".b.......b.",
		"..b.....b..",
		"....bbb....",
		"...bbbbb...",
		"b.bbbbbbb.b",
		"..bbbbbbb..",
		"wwwwwwwwwww",
		"...........",
		"..wwwwwww..",
		"....www....",
	},
}

type scheme struct {
	name                                 string
	ground, frame, lit, shade, ink, heart color.NRGBA
}

func rgb(v uint32) color.NRGBA { return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255} }

// schemes: dusk is the mark, paper the same on a light page. The heart
// stays red and the sun and quill amber in both, so the marks read as one
// family whatever the frame does.
var schemes = []scheme{
	{"dusk", rgb(0x2A1B36), rgb(0xD9774B), rgb(0xF29E6B), rgb(0x150C1E), rgb(0xFFE8C2), rgb(0xFF5A5F)},
	{"paper", rgb(0xF4E6D0), rgb(0xC9774A), rgb(0xE39B6E), rgb(0xC9B597), rgb(0x5A3426), rgb(0xE5484D)},
}

// coinInk is each coin's and chain's own colour, the same in both
// schemes: usdc blue, tether green, solana's violet, teal and green, base
// blue, polygon purple, tron red, and 'c' the white drawn on a coin. Each
// is its maker's colour warmed toward merlin's cream, a fifth of the way
// and a third for base's electric blue and tron's red, the two loudest, so
// it is still theirs at a glance but sits with the heart and the dawn
// rather than shouting over them.
var coinInk = map[rune]color.NRGBA{
	'u': warm(0x2775CA, 5), 't': warm(0x26A17B, 5),
	'v': warm(0x9945FF, 5), 'm': warm(0x43B4CA, 5), 'g': warm(0x14F195, 5),
	'k': warm(0x0052FF, 3), 'p': warm(0x8247E5, 5), 'r': warm(0xEF0027, 3),
	'c': rgb(0xFFF8E6),
}

// warm is a colour one nth of the way to merlin's cream.
func warm(v uint32, nth int) color.NRGBA {
	c, to := rgb(v), rgb(0xFFE8C2)
	mix := func(a, b uint8) uint8 { return uint8((int(a)*(nth-1) + int(b)) / nth) }
	return color.NRGBA{mix(c.R, to.R), mix(c.G, to.G), mix(c.B, to.B), 255}
}

var (
	amber = rgb(0xFFB347)
	// gold is the frame for paying more: only the badge wears it.
	goldFrame, goldLit = rgb(0xD9A928), rgb(0xFFE07A)
)

const (
	cells = 32
	cell  = 4
	size  = cells * cell // 128, what Discord keeps an emoji or role icon at
	px    = 8            // a glyph pixel
)

func main() {
	out := filepath.Join("edge", "public", "art", "support")
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	for _, s := range schemes {
		for _, name := range []string{"badge", "previews", "early", "say", "usdc", "usdt", "sol", "base", "polygon", "tron"} {
			write(filepath.Join(out, name+"_"+s.name+".png"), tile(glyphs[name], s, s.frame, s.lit))
		}
		write(filepath.Join(out, "badge_gold_"+s.name+".png"), tile(glyphs["badge"], s, goldFrame, goldLit))
	}
	skin()
}

// tile is one mark on its frame.
func tile(g []string, s scheme, frame, lit color.NRGBA) *image.NRGBA {
	if err := balanced(g); err != nil {
		log.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	last := cells - 1
	for y := range cells {
		for x := range cells {
			dx, dy := min(x, last-x), min(y, last-y)
			var c color.NRGBA
			switch {
			case dx+dy < 2: // the cut corner
				continue
			case dx == 0 || dy == 0 || dx+dy == 2:
				c = frame
			case (x == 1 || y == 1) && x < last-1 && y < last-1:
				c = lit
			case x == last-1 || y == last-1:
				c = dim(frame)
			default:
				c = s.ground
			}
			fill(img, x*cell, y*cell, cell, c)
		}
	}
	// The ink is 11 glyph pixels of 8 on the 112 px ground (8 to 119): 12
	// px either side. The shadow goes first, one cell down and right.
	const at = 2*cell + (size-4*cell-len("...........")*px)/2
	for pass := range 2 {
		for gy, row := range g {
			for gx, ch := range row {
				if ch == '.' {
					continue
				}
				c := s.shade
				if pass == 1 {
					c = map[rune]color.NRGBA{'w': s.ink, 'a': s.heart, 'b': amber}[ch]
					if k, ok := coinInk[ch]; ok {
						c = k
					}
				}
				off := cell * (1 - pass)
				fill(img, at+gx*px+off, at+gy*px+off, px, c)
			}
		}
	}
	return img
}

func fill(img *image.NRGBA, x0, y0, n int, c color.NRGBA) {
	for y := y0; y < y0+n; y++ {
		for x := x0; x < x0+n; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

// balanced is whether a glyph is 11 by 11 with equal empty space on
// opposite sides, which is what centres it on the tile.
func balanced(g []string) error {
	if len(g) != 11 {
		return fmt.Errorf("%d rows, want 11", len(g))
	}
	x0, x1, y0, y1 := 11, -1, 11, -1
	for y, row := range g {
		if len(row) != 11 {
			return fmt.Errorf("row %d %q is not 11 wide", y, row)
		}
		for x, c := range row {
			_, coin := coinInk[c]
			switch {
			case c == '.':
			case c == 'w' || c == 'a' || c == 'b' || coin:
				x0, x1, y0, y1 = min(x0, x), max(x1, x), min(y0, y), max(y1, y)
			default:
				return fmt.Errorf("unknown ink %q at %d,%d", c, x, y)
			}
		}
	}
	if x0 != 10-x1 || y0 != 10-y1 {
		return fmt.Errorf("margins left %d right %d top %d bottom %d; it would sit off centre", x0, 10-x1, y0, 10-y1)
	}
	return nil
}

// dim is the frame's shaded side, at 60%.
func dim(c color.NRGBA) color.NRGBA {
	return color.NRGBA{uint8(int(c.R) * 6 / 10), uint8(int(c.G) * 6 / 10), uint8(int(c.B) * 6 / 10), 255}
}

// write saves img as an indexed PNG at best compression, with nothing but
// pixels in it: no text chunks, no paths.
func write(path string, img *image.NRGBA) {
	index := map[color.NRGBA]uint8{}
	var pal color.Palette
	b := img.Bounds()
	out := image.NewPaletted(b, nil)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			i, ok := index[c]
			if !ok {
				i = uint8(len(pal))
				index[c] = i
				pal = append(pal, c)
			}
			out.SetColorIndex(x, y, i)
		}
	}
	out.Palette = pal
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(f, out); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	fmt.Println(filepath.ToSlash(path))
}
