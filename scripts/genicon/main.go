// Command genicon draws the Orole mark: a lighthouse beam over a horizon line
// on a dark navy field. It is original artwork, rendered with 4x4 supersampling.
//
//	go run ./scripts/genicon
//
// writes resources/icon.png (1024x1024, packaging icon) and
// cmd/desktop/window-icon.png (256x256, embedded window icon).
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

var (
	navy   = [3]float64{7, 11, 20}
	accent = [3]float64{94, 200, 230}
)

// shape reports coverage 0..1 at (x,y) in unit space [0,1]^2 and its alpha-weighted accent contribution.
func sample(x, y float64) (alpha float64) {
	const (
		hy    = 0.56 // horizon y
		lampR = 0.105
	)
	a := 0.0
	// Horizon line.
	if math.Abs(y-hy) < 0.014 && x > 0.17 && x < 0.83 {
		a = math.Max(a, 1)
	}
	// Lamp: half disc resting on the horizon.
	dx, dy := x-0.5, y-hy+0.035
	if dx*dx+dy*dy < lampR*lampR && y < hy-0.014 {
		a = math.Max(a, 1)
	}
	// Beams: two wedges fanning out from the lamp, fading with distance.
	for _, dir := range []float64{-1, 1} {
		bx := (x - 0.5) * dir
		by := hy - 0.06 - y
		if bx > lampR*0.6 && by > -0.02 {
			// wedge between slopes 0.05 and 0.34 above the horizontal
			s := by / bx
			if s > 0.03 && s < 0.30 && bx < 0.36 {
				a = math.Max(a, 0.55*(1-bx/0.36))
			}
		}
	}
	// Reflection ticks under the horizon.
	for i, w := range []float64{0.30, 0.18, 0.08} {
		ty := hy + 0.05 + float64(i)*0.045
		if math.Abs(y-ty) < 0.008 && math.Abs(x-0.5) < w/2 {
			a = math.Max(a, 0.5-0.12*float64(i))
		}
	}
	return a
}

func render(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	const ss = 4
	for py := range size {
		for px := range size {
			var acc float64
			for sy := range ss {
				for sx := range ss {
					acc += sample((float64(px)+(float64(sx)+0.5)/ss)/float64(size), (float64(py)+(float64(sy)+0.5)/ss)/float64(size))
				}
			}
			acc /= ss * ss
			// Soft vignette keeps the navy from looking flat.
			vx, vy := float64(px)/float64(size)-0.5, float64(py)/float64(size)-0.5
			lift := 10 * math.Max(0, 1-2.2*math.Sqrt(vx*vx+vy*vy))
			var c [3]float64
			for i := range c {
				c[i] = navy[i]*(1-acc) + accent[i]*acc + lift*(1-acc)
			}
			img.SetRGBA(px, py, color.RGBA{uint8(c[0]), uint8(c[1]), uint8(c[2]), 255})
		}
	}
	return img
}

func write(path string, size int) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, render(size)); err != nil {
		log.Fatal(err)
	}
}

func main() {
	if err := os.MkdirAll("resources", 0o755); err != nil {
		log.Fatal(err)
	}
	write("resources/icon.png", 1024)
	write("cmd/desktop/window-icon.png", 256)
}
