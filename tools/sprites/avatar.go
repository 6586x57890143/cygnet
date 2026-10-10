package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"
)

// The avatar is drawn, not computed: art/source/cygnet_avatar_source.png is the
// original, a cygnet in a disc of dusk water and reeds on a flat navy field.
// This keeps the whole disc, drops the field (the profile picture redraws it
// flat), resamples onto the grid and reduces the disc to its own few dozen
// colours, so the source stays the one thing a person edits and the palette
// is the drawing's.

const avatarSource = "art/source/cygnet_avatar_source.png"

// tones is how many colours the disc keeps. Enough for the down's soft
// shading, few enough that a 256px icon stays under brand's 10 KB budget.
const tones = 40

// fieldAt reports whether a source pixel is the navy field: within a hair of
// fieldInk, which the disc's darkest water never comes near.
func fieldAt(c color.NRGBA) bool {
	d := func(a, b uint8) int { return max(int(a), int(b)) - min(int(a), int(b)) }
	return d(c.R, fieldInk.R)+d(c.G, fieldInk.G)+d(c.B, fieldInk.B) < 12
}

// grid is the avatar's logical resolution, in cells across. Every avatar
// output is a whole multiple of it: the profile picture is 128x8 = 1024 and
// the mood icons 128x2 = 256, so a cell is always a hard square of one size.
const grid = 128

// bird returns the source's disc, quantised, on transparency at grid
// resolution.
func bird() (*image.NRGBA, error) {
	f, err := os.Open(avatarSource)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	src, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			c := color.NRGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			if !fieldAt(c) {
				out.SetNRGBA(x, y, c)
			}
		}
	}
	return quantise(cells(out, grid), tones), nil
}

// quantise maps every opaque pixel to the nearest of k colours found by
// k-means over the image itself. The seeds are spread evenly by brightness
// and the iteration count is fixed, so every run picks the same colours.
func quantise(img *image.NRGBA, k int) *image.NRGBA {
	var px []color.NRGBA
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if c := img.NRGBAAt(x, y); c.A != 0 {
				px = append(px, c)
			}
		}
	}
	sorted := append([]color.NRGBA(nil), px...)
	sort.SliceStable(sorted, func(i, j int) bool { return lum(sorted[i]) < lum(sorted[j]) })
	centres := make([][3]float64, k)
	for i := range centres {
		c := sorted[(2*i+1)*len(sorted)/(2*k)]
		centres[i] = [3]float64{float64(c.R), float64(c.G), float64(c.B)}
	}
	nearest := func(c color.NRGBA) int {
		best, bd := 0, math.MaxFloat64
		for i, m := range centres {
			dr, dg, db := float64(c.R)-m[0], float64(c.G)-m[1], float64(c.B)-m[2]
			if d := dr*dr + dg*dg + db*db; d < bd {
				best, bd = i, d
			}
		}
		return best
	}
	for range 20 {
		sum := make([][4]float64, k)
		for _, c := range px {
			i := nearest(c)
			sum[i][0] += float64(c.R)
			sum[i][1] += float64(c.G)
			sum[i][2] += float64(c.B)
			sum[i][3]++
		}
		for i, s := range sum {
			if s[3] > 0 {
				centres[i] = [3]float64{s[0] / s[3], s[1] / s[3], s[2] / s[3]}
			}
		}
	}
	out := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if c := img.NRGBAAt(x, y); c.A != 0 {
				m := centres[nearest(c)]
				out.SetNRGBA(x, y, color.NRGBA{uint8(math.Round(m[0])), uint8(math.Round(m[1])), uint8(math.Round(m[2])), 0xFF})
			}
		}
	}
	return out
}

func lum(c color.NRGBA) float64 {
	return 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
}

// cells resamples src onto an n by n grid. A cell is opaque when most of its
// source pixels are, and then takes the colour most of those have. Nearest
// neighbour would sample one pixel per cell, and the source's own cells are
// about 9.8px, not a whole number, so its output mixed cells of two widths.
// This keeps every cell the same size and adds no colour.
func cells(src *image.NRGBA, n int) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, n, n))
	for cy := range n {
		for cx := range n {
			count := map[color.NRGBA]int{}
			var best color.NRGBA
			top, opaque, all := 0, 0, 0
			for y := cy * b.Dy() / n; y < (cy+1)*b.Dy()/n; y++ {
				for x := cx * b.Dx() / n; x < (cx+1)*b.Dx()/n; x++ {
					all++
					c := src.NRGBAAt(b.Min.X+x, b.Min.Y+y)
					if c.A == 0 {
						continue
					}
					opaque++
					count[c]++
					if count[c] > top {
						best, top = c, count[c]
					}
				}
			}
			if 2*opaque > all {
				dst.SetNRGBA(cx, cy, best)
			}
		}
	}
	return dst
}

// pfp is the profile picture at grid resolution: the disc on the flat field.
func pfp(b *image.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(b.Bounds())
	flat(img, fieldInk)
	over(img, b)
	return img
}

// centred moves the disc so its bounding box sits in the middle of the grid,
// to the nearest whole cell.
func centred(b *image.NRGBA) *image.NRGBA {
	box := image.Rectangle{}
	r := b.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if b.NRGBAAt(x, y).A != 0 {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	d := image.Pt((r.Dx()-box.Dx())/2-box.Min.X, (r.Dy()-box.Dy())/2-box.Min.Y)
	out := image.NewNRGBA(r)
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			out.SetNRGBA(x+d.X, y+d.Y, b.NRGBAAt(x, y))
		}
	}
	return out
}
