package colorqr_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"git.foxden.network/FoxDen/hcc2d/pkg/colorqr"
)

var schemes = []colorqr.Scheme{colorqr.FourColor, colorqr.EightColor}

func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + i/7)
	}
	return b
}

func mustSymbol(t *testing.T, data []byte, o *colorqr.EncodeOptions) *colorqr.Symbol {
	t.Helper()
	sym, err := colorqr.NewSymbol(data, o)
	if err != nil {
		t.Fatalf("NewSymbol: %v", err)
	}
	return sym
}

func mustDecode(t *testing.T, img image.Image, want []byte, o *colorqr.DecodeOptions) *colorqr.Result {
	t.Helper()
	res, err := colorqr.DecodeImage(img, o)
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if !bytes.Equal(res.Data, want) {
		t.Fatalf("decoded %d bytes, want %d (content mismatch)", len(res.Data), len(want))
	}
	return res
}

func TestRoundTripPNG(t *testing.T) {
	for _, s := range schemes {
		for _, data := range [][]byte{{}, []byte("Hello, HCC2D!"), {0, 0, 0xff, 0}, payload(256), []byte(strings.Repeat("colorqr ", 100))} {
			t.Run(fmt.Sprintf("%s/%d", s, len(data)), func(t *testing.T) {
				var buf bytes.Buffer
				if err := colorqr.Encode(&buf, data, &colorqr.EncodeOptions{Scheme: s}); err != nil {
					t.Fatal(err)
				}
				res, err := colorqr.Decode(&buf, nil)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(res.Data, data) || res.Scheme != s {
					t.Fatalf("got %d bytes, scheme %s", len(res.Data), res.Scheme)
				}
			})
		}
	}
}

func TestAllVersionsAndLevels(t *testing.T) {
	for _, s := range schemes {
		for v := colorqr.MinVersion; v <= colorqr.MaxVersion; v++ {
			if s == colorqr.EightColor && v == 1 {
				continue
			}
			level := colorqr.ECLevel(v % 4)
			t.Run(fmt.Sprintf("%s/v%d/%s", s, v, level), func(t *testing.T) {
				if testing.Short() && v%5 != 0 {
					t.Skip()
				}
				t.Parallel()
				o := &colorqr.EncodeOptions{Scheme: s, Level: level, MinVersion: v}
				empty := mustSymbol(t, nil, o)
				if empty.Version != v {
					t.Fatalf("got version %d", empty.Version)
				}
				// Fill the symbol to capacity.
				n := colorqr.Capacity(v, s, level)
				data := payload(n)
				sym := mustSymbol(t, data, o)
				if sym.Version != v {
					t.Fatalf("capacity %d bytes landed in version %d", n, sym.Version)
				}
				if s2, err := colorqr.NewSymbol(payload(n+1), o); err == nil && s2.Version == v {
					t.Fatalf("capacity+1 still fits version %d", v)
				}
				res := mustDecode(t, sym.Image(3, 4), data, nil)
				if res.Version != v || res.Level != level || res.Scheme != s {
					t.Fatalf("decoded v%d %s %s", res.Version, res.Level, res.Scheme)
				}
			})
		}
	}
}

// Capacity grows with the number of colors: a 4-color symbol must hold at
// least 1.5× and an 8-color symbol at least 2.5× the payload of the
// equivalent QR symbol (ideally 2× and 3×, minus the palette patterns).
func TestCapacity(t *testing.T) {
	for _, tc := range []struct {
		v        int
		qrLevelL int // QR byte-mode capacity at level L
	}{{1, 17}, {10, 271}, {40, 2953}} {
		c4 := colorqr.Capacity(tc.v, colorqr.FourColor, colorqr.ECLow)
		c8 := colorqr.Capacity(tc.v, colorqr.EightColor, colorqr.ECLow)
		t.Logf("v%d: QR-L %d bytes, 4-color %d bytes, 8-color %d bytes", tc.v, tc.qrLevelL, c4, c8)
		if c4 < 3*tc.qrLevelL/2 || (tc.v > 1 && c8 < 5*tc.qrLevelL/2) {
			t.Errorf("v%d: capacity too low", tc.v)
		}
	}
}

func TestTransforms(t *testing.T) {
	data := []byte("The quick brown fox jumps over the lazy dog. 0123456789")
	for _, s := range schemes {
		sym := mustSymbol(t, data, &colorqr.EncodeOptions{Scheme: s, Level: colorqr.ECMedium, MinVersion: 5})
		base := sym.Image(8, 4)
		cases := map[string]image.Image{
			"module 2px":    sym.Image(2, 4),
			"module 5px":    sym.Image(5, 1),
			"rotate 90":     rotate90(base),
			"rotate 180":    rotate90(rotate90(base)),
			"rotate 270":    rotate90(rotate90(rotate90(base))),
			"rotate 17deg":  rotate(base, 17),
			"rotate -40deg": rotate(base, -40),
			"offset canvas": offset(base, 123, 45),
			"scale 1.37":    rotate(sym.Image(6, 4), 0.0001, 1.37),
			"noise":         noisy(base, 25, 1),
			"color cast":    colorCast(base),
			"jpeg q85":      jpegRoundTrip(t, base, 85),
		}
		for name, img := range cases {
			t.Run(fmt.Sprintf("%s/%s", s, name), func(t *testing.T) {
				mustDecode(t, img, data, nil)
			})
		}
	}
}

func TestDamagedModulesAreCorrected(t *testing.T) {
	data := payload(300)
	for _, s := range schemes {
		for _, level := range []colorqr.ECLevel{colorqr.ECLow, colorqr.ECHigh} {
			t.Run(fmt.Sprintf("%s/%s", s, level), func(t *testing.T) {
				sym := mustSymbol(t, data, &colorqr.EncodeOptions{Scheme: s, Level: level})
				img := sym.Image(4, 4)
				// Recolor random modules in the lower right quadrant (away
				// from all finder, format and palette patterns). The number
				// of hits is 3% (L) or 15% (H) of the symbol's module count;
				// some land on the same module or keep its color.
				frac := map[colorqr.ECLevel]float64{colorqr.ECLow: 0.03, colorqr.ECHigh: 0.15}[level]
				rng := rand.New(rand.NewPCG(7, uint64(s)))
				pal := s.DefaultPalette()
				n := sym.Size()
				hits := int(frac * float64(n*n))
				for range hits {
					x := n/2 + rng.IntN(n/2-10)
					y := n/2 + rng.IntN(n/2-10)
					c := pal[rng.IntN(len(pal))]
					draw.Draw(img, image.Rect((x+4)*4, (y+4)*4, (x+5)*4, (y+5)*4), image.NewUniform(c), image.Point{}, draw.Src)
				}
				res := mustDecode(t, img, data, nil)
				if res.Corrected == 0 {
					t.Fatalf("expected corrections")
				}
				t.Logf("v%d, %d recolored modules, %d bytes corrected", res.Version, hits, res.Corrected)
			})
		}
	}
}

func TestClassifiers(t *testing.T) {
	data := payload(200)
	for _, s := range schemes {
		img := noisy(mustSymbol(t, data, &colorqr.EncodeOptions{Scheme: s, Level: colorqr.ECMedium}).Image(6, 4), 20, 2)
		for _, c := range []colorqr.Classifier{colorqr.KMeans, colorqr.MinDistance} {
			mustDecode(t, img, data, &colorqr.DecodeOptions{Classifier: c})
		}
	}
}

func TestCustomPalette(t *testing.T) {
	pal := colorqr.Palette{
		{250, 250, 240, 255},
		{200, 40, 40, 255},
		{40, 90, 200, 255},
		{20, 20, 30, 255},
	}
	data := []byte("custom palette")
	sym := mustSymbol(t, data, &colorqr.EncodeOptions{Palette: pal})
	mustDecode(t, sym.Image(6, 4), data, nil)
}

func TestErrors(t *testing.T) {
	if _, err := colorqr.NewSymbol(nil, &colorqr.EncodeOptions{Palette: colorqr.FourColorPalette[:2]}); err == nil {
		t.Error("expected error for mismatched palette size")
	}
	if _, err := colorqr.NewSymbol(nil, &colorqr.EncodeOptions{Scheme: 16}); err == nil {
		t.Error("expected error for unsupported scheme")
	}
	if _, err := colorqr.NewSymbol(payload(20000), &colorqr.EncodeOptions{Scheme: colorqr.EightColor}); err == nil {
		t.Error("expected error for oversized payload")
	}
	if _, err := colorqr.Decode(strings.NewReader("not an image"), nil); err == nil {
		t.Error("expected error decoding garbage")
	}
	blank := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(blank, blank.Bounds(), image.White, image.Point{}, draw.Src)
	if _, err := colorqr.DecodeImage(blank, nil); err == nil {
		t.Error("expected error decoding a blank image")
	}
}

// ── image helpers ───────────────────────────────────────────────────────────

func rotate90(src image.Image) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(b.Max.Y-1-y, x-b.Min.X, src.At(x, y))
		}
	}
	return dst
}

// rotate rotates src by deg degrees and scales it, with bilinear sampling
// on a white background.
func rotate(src image.Image, deg float64, scale ...float64) *image.RGBA {
	k := 1.0
	if len(scale) > 0 {
		k = scale[0]
	}
	b := src.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	a := deg * math.Pi / 180
	cos, sin := math.Cos(a), math.Sin(a)
	n := int(k*(w*math.Abs(cos)+h*math.Abs(sin))) + 2
	dst := image.NewRGBA(image.Rect(0, 0, n, n))
	c := float64(n) / 2
	for y := range n {
		for x := range n {
			dx, dy := (float64(x)+0.5-c)/k, (float64(y)+0.5-c)/k
			sx := cos*dx + sin*dy + w/2 - 0.5
			sy := -sin*dx + cos*dy + h/2 - 0.5
			dst.Set(x, y, bilinear(src, sx, sy))
		}
	}
	return dst
}

func bilinear(src image.Image, x, y float64) color.RGBA {
	b := src.Bounds()
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	var acc [3]float64
	for _, p := range []struct {
		x, y int
		w    float64
	}{{x0, y0, (1 - fx) * (1 - fy)}, {x0 + 1, y0, fx * (1 - fy)}, {x0, y0 + 1, (1 - fx) * fy}, {x0 + 1, y0 + 1, fx * fy}} {
		c := color.RGBA{255, 255, 255, 255}
		if (image.Point{p.x, p.y}).In(b) {
			c = color.RGBAModel.Convert(src.At(p.x, p.y)).(color.RGBA)
		}
		acc[0] += p.w * float64(c.R)
		acc[1] += p.w * float64(c.G)
		acc[2] += p.w * float64(c.B)
	}
	return color.RGBA{uint8(acc[0] + 0.5), uint8(acc[1] + 0.5), uint8(acc[2] + 0.5), 255}
}

func offset(src image.Image, dx, dy int) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()+dx+37, b.Dy()+dy+11))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(dst, b.Add(image.Pt(dx, dy)), src, b.Min, draw.Src)
	return dst
}

func mapPixels(src image.Image, f func(x, y int, c [3]float64) [3]float64) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.RGBAModel.Convert(src.At(x, y)).(color.RGBA)
			o := f(x, y, [3]float64{float64(c.R), float64(c.G), float64(c.B)})
			for i := range o {
				o[i] = math.Max(0, math.Min(255, o[i]))
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(o[0]), uint8(o[1]), uint8(o[2]), 255})
		}
	}
	return dst
}

func noisy(src image.Image, sigma float64, seed uint64) *image.RGBA {
	rng := rand.New(rand.NewPCG(seed, 99))
	return mapPixels(src, func(_, _ int, c [3]float64) [3]float64 {
		for i := range c {
			c[i] += rng.NormFloat64() * sigma
		}
		return c
	})
}

// colorCast imitates a print & scan channel: reduced contrast, a warm
// tint, gamma and uneven illumination across the image.
func colorCast(src image.Image) *image.RGBA {
	w := float64(src.Bounds().Dx())
	return mapPixels(src, func(x, _ int, c [3]float64) [3]float64 {
		light := 0.85 + 0.15*float64(x)/w
		tint := [3]float64{1.0, 0.92, 0.78}
		for i := range c {
			v := 30 + c[i]*0.75*tint[i]
			c[i] = 255 * math.Pow(v/255, 1.3) * light
		}
		return c
	})
}

func jpegRoundTrip(t *testing.T, src image.Image, q int) image.Image {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: q}); err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	return img
}
