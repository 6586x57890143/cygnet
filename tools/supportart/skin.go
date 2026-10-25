package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"log"
	"math"
	"os"
	"path/filepath"
)

// The merlin's skin: her colours, read off two photographs of a real
// merlin, as grids in the model's own units, so each point of
// edge/public/merlin.js takes the colour of the bird where it falls.
//
//   art/source/merlin_side.jpg: "Merlin" 54309288128, US Fish and Wildlife
//   Service Mountain-Prairie, public domain; a crop of the original (1000,
//   180) to (1800, 980), side on, the perch point at (290, 620) and her
//   crown 568 px above it. Her colour seen from the side, by x and y.
//
//   art/source/merlin_front.jpg: "Merlin, January 27 2021", Warren
//   Bielenberg, Great Smoky Mountains National Park, public domain; facing
//   the camera, her feet at (479, 345) and her crown 241 px above them. Her
//   colour seen from the front, by z and y.
//
// Units are merlin.js's: her height from the perch to her crown is 1, x
// runs from the perch point toward her tail, y up, z across her (the
// front photo's right is her z positive).

// view is one photo and how it maps onto her.
type view struct {
	file           string
	ox, oy, scale  float64
	u0, u1, v0, v1 float64
	hidden         func(u, v float64) bool
	name, desc     string
}

const skinStep = 0.012

var views = []view{
	{
		file: "merlin_side.jpg", ox: 290, oy: 620, scale: 568,
		u0: -0.42, u1: 0.86, v0: -0.3, v1: 1.05,
		// The branch: a band under the line its top edge runs along,
		// (-0.035, 0.018) to (0.898, 0.511), apart from the tail that
		// hangs below it.
		hidden: func(x, y float64) bool {
			top := 0.018 + (x+0.035)/0.933*0.493
			tail := x > 0.3 && y < top-0.12
			return x > -0.06 && y < top+0.045 && y > top-0.17 && !tail
		},
		name: "side", desc: "seen from the side, by x and y",
	},
	{
		file: "merlin_front.jpg", ox: 479, oy: 345, scale: 241,
		u0: -0.42, u1: 0.42, v0: -0.8, v1: 1.05,
		// The branch, crossing in front of her feet.
		hidden: func(z, y float64) bool { return y < 0.065 && y > -0.21 },
		name: "front", desc: "seen from the front, by z and y",
	},
}

// sky is the clear blue both photos were taken against.
func sky(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	R, G, B := int(r>>8), int(g>>8), int(b>>8)
	return B > R+40 && B > G+10
}

// yellow is her feet and cere, which stay hers where they cross a branch.
func yellow(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	R, G, B := int(r>>8), int(g>>8), int(b>>8)
	return R > 170 && G > 100 && B < 90 && R > G
}

// sample is one view as a grid, each cell the mean colour of the bird
// under it. A cell off the bird or hidden takes the nearest cell at or
// above it that is on her, so a point that lands just off her outline
// still takes her colour, and one behind the branch takes her feathers
// over it, not the bark under it.
func sample(v view) (cols, rows int, rgb []byte) {
	f, err := os.Open(filepath.Join("art", "source", v.file))
	if err != nil {
		log.Fatal(err)
	}
	img, _, err := image.Decode(f)
	if err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	cols = int(math.Round((v.u1 - v.u0) / skinStep))
	rows = int(math.Round((v.v1 - v.v0) / skinStep))
	type cell struct {
		r, g, b float64
		ok      bool
	}
	grid := make([]cell, cols*rows)
	for j := range rows {
		for i := range cols {
			u0 := v.u0 + float64(i)*skinStep
			v0 := v.v1 - float64(j+1)*skinStep
			var r, g, b, n float64
			for py := int(v.oy - (v0+skinStep)*v.scale); py < int(v.oy-v0*v.scale); py++ {
				for px := int(v.ox + u0*v.scale); px < int(v.ox+(u0+skinStep)*v.scale); px++ {
					c := img.At(px, py)
					mu, mv := (float64(px)-v.ox)/v.scale, (v.oy-float64(py))/v.scale
					if sky(c) || (v.hidden(mu, mv) && !yellow(c)) {
						continue
					}
					cr, cg, cb, _ := c.RGBA()
					r, g, b, n = r+float64(cr>>8), g+float64(cg>>8), b+float64(cb>>8), n+1
				}
			}
			if n > 1 {
				grid[j*cols+i] = cell{r / n, g / n, b / n, true}
			}
		}
	}
	var out bytes.Buffer
	for j := range rows {
		for i := range cols {
			c := grid[j*cols+i]
			if !c.ok {
				best := math.Inf(1)
				for jj := max(0, j-25); jj <= j; jj++ {
					for ii := max(0, i-25); ii < min(cols, i+26); ii++ {
						if d := float64((jj-j)*(jj-j) + (ii-i)*(ii-i)); grid[jj*cols+ii].ok && d < best {
							best, c = d, grid[jj*cols+ii]
						}
					}
				}
			}
			out.Write([]byte{uint8(c.r), uint8(c.g), uint8(c.b)})
		}
	}
	return cols, rows, out.Bytes()
}

// skin writes edge/public/merlin-skin.js: one grid per view.
func skin() {
	var js bytes.Buffer
	js.WriteString(`// merlin's skin: her colours from two photographs of real merlins, as
// grids in merlin.js's units (tools/supportart/skin.go names the photos,
// both public domain). Written by go run ./tools/supportart; edit that,
// not this.
`)
	for _, v := range views {
		cols, rows, rgb := sample(v)
		fmt.Fprintf(&js, "// %s\nexport const %s = { u0: %g, v1: %g, step: %g, cols: %d, rows: %d, rgb: %q };\n",
			v.desc, v.name, v.u0, v.v1, skinStep, cols, rows, base64.StdEncoding.EncodeToString(rgb))
		fmt.Println(v.name, cols, "x", rows)
	}
	path := filepath.Join("edge", "public", "merlin-skin.js")
	if err := os.WriteFile(path, js.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println(filepath.ToSlash(path))
}
