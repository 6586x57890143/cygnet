package main

import (
	"image"
	"image/color"
)

// The palette and the banner. Every colour is taken from the avatar's
// source: the navy field, the dusk sky and water, the cygnet's cream down
// and slate bill, and the reeds. High resolution pixel art: flat fields and
// texture from clean clusters keyed to a fixed hash, so every run renders
// the same image and a palette change is one rerun.

func rgb(h uint32) color.NRGBA {
	return color.NRGBA{uint8(h >> 16), uint8(h >> 8), uint8(h), 0xFF}
}

var (
	// Dusk, darkest to lightest: the field, then water, then sky.
	slate = []color.NRGBA{rgb(0x2B3249), rgb(0x38425F), rgb(0x606E87), rgb(0x7C889E), rgb(0x979CAA)}
	// The cygnet's down, shadow to light.
	bone = []color.NRGBA{rgb(0xCBBEBA), rgb(0xE1D5CC)}
	// The reeds: sage and dry tan.
	reed = []color.NRGBA{rgb(0x4E5962), rgb(0x7A7477)}
	// The eye.
	lineInk = rgb(0x0E1018)

	fieldInk = slate[1]
)

func flat(img *image.NRGBA, c color.NRGBA) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

func over(dst, src *image.NRGBA) {
	r := src.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if c := src.NRGBAAt(x, y); c.A != 0 {
				dst.SetNRGBA(x, y, c)
			}
		}
	}
}

// Banner geometry in logical pixels: 320x110, written at 4x (1280x440).
const (
	bannerW, bannerH = 320, 110
	horizon          = 46
)

// banner is the avatar's world opened out: dusk sky in flat bands over still
// water with ripple streaks, and reeds standing at the right edge. The left
// is kept quiet, since Discord lays the avatar over it.
func banner() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, bannerW, bannerH))
	for y := range bannerH {
		// Sky lightens toward the horizon in three bands; water darkens
		// away from it in three.
		var c color.NRGBA
		switch {
		case y < horizon/3:
			c = slate[2]
		case y < 2*horizon/3:
			c = slate[3]
		case y < horizon:
			c = slate[4]
		case y < horizon+16:
			c = slate[2]
		case y < horizon+40:
			c = slate[1]
		default:
			c = slate[0]
		}
		for x := range bannerW {
			img.SetNRGBA(x, y, c)
		}
	}
	// Ripples: short level streaks, one lighter tone than the water under
	// them, fewer and longer further from the horizon.
	for y := horizon + 2; y < bannerH; y += 3 {
		for x := 0; x < bannerW; x++ {
			h := hash(x/6, y)
			if h%7 != 0 {
				continue
			}
			n := 3 + int(h>>8)%6
			c := slate[min(len(slate)-1, 1+tier(y))]
			for i := 0; i < n && x+i < bannerW; i++ {
				img.SetNRGBA(x+i, y, c)
			}
			x += n
		}
	}
	// Reeds: tapering stalks rooted in the water, two cells wide at the
	// base and one at the tip, leaning a little with height.
	for i := range 14 {
		h := hash(i, 99)
		x0 := 236 + int(h%80)
		top := 12 + int(h>>8)%36
		ink := reed[int(h>>16)%len(reed)]
		for y := top; y < bannerH-20+int(h>>4)%12; y++ {
			x := x0 + (y-top)/14 - 2
			img.SetNRGBA(clampi(x, 0, bannerW-1), y, ink)
			if y > top+(bannerH-top)/2 {
				img.SetNRGBA(clampi(x+1, 0, bannerW-1), y, ink)
			}
		}
	}
	return img
}

// tier is which water band y is in, 0 nearest the horizon.
func tier(y int) int {
	switch {
	case y < horizon+16:
		return 2
	case y < horizon+40:
		return 1
	default:
		return 0
	}
}

func clampi(v, lo, hi int) int { return max(lo, min(hi, v)) }

// hash is a fixed per-pixel noise, so the texture is the same on every run.
func hash(x, y int) uint32 {
	h := uint32(x)*374761393 + uint32(y)*668265263
	h = (h ^ (h >> 13)) * 1274126177
	return h ^ (h >> 16)
}
