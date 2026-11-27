package colorqr

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"testing"
)

// warp renders src in perspective: its corners land on dst (top-left,
// top-right, bottom-right, bottom-left) in a w×h image, with bilinear
// sampling on a background of bg.
func warp(src image.Image, dst [4][2]float64, w, h int, bg color.RGBA) *image.RGBA {
	b := src.Bounds()
	sw, sh := float64(b.Dx()), float64(b.Dy())
	// Map output pixels back to source pixels.
	inv, ok := homographyFrom(dst, [4][2]float64{{0, 0}, {sw, 0}, {sw, sh}, {0, sh}})
	if !ok {
		panic("degenerate warp")
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			sx, sy := inv.apply(float64(x)+0.5, float64(y)+0.5)
			out.SetRGBA(x, y, bilinearAt(src, sx-0.5, sy-0.5, bg))
		}
	}
	return out
}

func bilinearAt(src image.Image, x, y float64, bg color.RGBA) color.RGBA {
	b := src.Bounds()
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	var acc [3]float64
	for _, p := range []struct {
		x, y int
		w    float64
	}{{x0, y0, (1 - fx) * (1 - fy)}, {x0 + 1, y0, fx * (1 - fy)}, {x0, y0 + 1, (1 - fx) * fy}, {x0 + 1, y0 + 1, fx * fy}} {
		c := bg
		if (image.Point{p.x, p.y}).In(b) {
			c = color.RGBAModel.Convert(src.At(p.x, p.y)).(color.RGBA)
		}
		acc[0] += p.w * float64(c.R)
		acc[1] += p.w * float64(c.G)
		acc[2] += p.w * float64(c.B)
	}
	return color.RGBA{uint8(acc[0] + 0.5), uint8(acc[1] + 0.5), uint8(acc[2] + 0.5), 255}
}

// photo imitates a phone picture: lighting that falls off across the
// frame, a color cast, a slight blur and sensor noise.
func photo(src *image.RGBA, seed uint64) *image.RGBA {
	rng := rand.New(rand.NewPCG(seed, 5))
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(b)
	tint := [3]float64{1.0, 0.93, 0.8}
	for y := range h {
		for x := range w {
			// 3×3 box blur.
			var acc [3]float64
			n := 0.0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if xx, yy := x+dx, y+dy; xx >= 0 && yy >= 0 && xx < w && yy < h {
						c := src.RGBAAt(xx, yy)
						acc[0] += float64(c.R)
						acc[1] += float64(c.G)
						acc[2] += float64(c.B)
						n++
					}
				}
			}
			light := 0.45 + 0.55*(1-0.6*float64(x)/float64(w)-0.4*float64(y)/float64(h))
			var o [3]uint8
			for i := range acc {
				v := (20+acc[i]/n*0.8)*tint[i]*light + rng.NormFloat64()*6
				o[i] = uint8(math.Max(0, math.Min(255, v)))
			}
			out.SetRGBA(x, y, color.RGBA{o[0], o[1], o[2], 255})
		}
	}
	return out
}

func TestPerspectivePhotos(t *testing.T) {
	data := []byte("https://example.com/hcc2d?colors=many&perspective=yes")
	bg := color.RGBA{120, 110, 100, 255}
	shots := map[string][4][2]float64{
		"tilted left":  {{80, 60}, {560, 110}, {540, 520}, {100, 590}},
		"tilted back":  {{120, 80}, {520, 80}, {590, 560}, {50, 560}},
		"rotated skew": {{300, 30}, {610, 300}, {330, 620}, {30, 310}},
	}
	for _, s := range []Scheme{FourColor, EightColor} {
		for _, v := range []int{3, 8} {
			sym, err := NewSymbol(data, &EncodeOptions{Scheme: s, Level: ECMedium, MinVersion: v})
			if err != nil {
				t.Fatal(err)
			}
			src := sym.Image(10, 3)
			for name, corners := range shots {
				t.Run(fmt.Sprintf("%s/v%d/%s", s, v, name), func(t *testing.T) {
					img := photo(warp(src, corners, 640, 640, bg), 3)
					res, err := DecodeImage(img, nil)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(res.Data, data) || res.Scheme != s {
						t.Fatalf("got %q as %s", res.Data, res.Scheme)
					}
					t.Logf("v%d, %d bytes corrected", res.Version, res.Corrected)
				})
			}
		}
	}
}

func TestModesRoundTrip(t *testing.T) {
	for _, c := range []struct {
		data string
		mode Mode
	}{
		{"31415926535897932384626433832795028841971", ModeNumeric},
		{"HTTPS://EXAMPLE.COM/HCC2D", ModeAlphanumeric},
		{"Grüße, 世界", ModeByte},
	} {
		for _, s := range []Scheme{FourColor, EightColor} {
			sym, err := NewSymbol([]byte(c.data), &EncodeOptions{Scheme: s})
			if err != nil {
				t.Fatal(err)
			}
			if sym.Mode != c.mode {
				t.Errorf("%q: mode %s, want %s", c.data, sym.Mode, c.mode)
			}
			res, err := DecodeImage(sym.Image(4, 4), nil)
			if err != nil || string(res.Data) != c.data {
				t.Fatalf("%q %s: got %q, %v", c.data, s, res.Data, err)
			}
		}
	}
	// Numeric payloads pack 3.3 bits per digit, not 8.
	if n, b := CapacityMode(10, FourColor, ECLow, ModeNumeric), Capacity(10, FourColor, ECLow); n < 2*b {
		t.Errorf("numeric capacity %d, byte capacity %d", n, b)
	}
}

func TestCornersLocateSymbol(t *testing.T) {
	sym, err := NewSymbol([]byte("corners"), nil)
	if err != nil {
		t.Fatal(err)
	}
	const px, qz = 5, 4
	res, err := DecodeImage(sym.Image(px, qz), nil)
	if err != nil {
		t.Fatal(err)
	}
	lo, hi := float64((qz-1)*px), float64((qz+sym.Size()+1)*px)
	want := [4]Point{{lo, lo}, {hi, lo}, {hi, hi}, {lo, hi}}
	for i, c := range res.Corners {
		if math.Hypot(c.X-want[i].X, c.Y-want[i].Y) > 1 {
			t.Errorf("corner %d at %v, want %v", i, c, want[i])
		}
	}
}
