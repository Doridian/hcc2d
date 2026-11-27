package colorqr

import (
	"errors"
	"fmt"
	"image"
	_ "image/gif" // register the GIF, JPEG and PNG decoders for Decode
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
)

// Classifier selects how data modules are mapped to palette colors.
type Classifier int

const (
	// KMeans clusters all data modules, seeding the centroids with the
	// colors read from the palette patterns. The paper found it to have the
	// lowest byte error rate of the classifiers it studied.
	KMeans Classifier = iota
	// MinDistance assigns each module to the nearest palette-pattern color
	// (Euclidean distance in YUV), the paper's baseline classifier.
	MinDistance
)

// DecodeOptions controls decoding.
type DecodeOptions struct {
	Classifier Classifier
}

// Point is a position in image pixels.
type Point struct{ X, Y float64 }

// Result is a decoded symbol.
type Result struct {
	Data      []byte
	Version   int
	Scheme    Scheme
	Level     ECLevel
	Mask      int
	Corrected int // bytes fixed by Reed-Solomon

	// Corners are the image positions of the symbol's outer corners,
	// including the palette pattern ring: top-left, top-right,
	// bottom-right and bottom-left in the symbol's own orientation.
	Corners [4]Point
}

// Decode reads an image (PNG, JPEG or GIF) from r and decodes the symbol in
// it.
func Decode(r io.Reader, opts *DecodeOptions) (*Result, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	return DecodeImage(img, opts)
}

// DecodeImage locates and decodes a symbol in img. The symbol may be
// scaled, rotated and seen in perspective: the module grid is mapped from
// the three finder patterns and, for versions 2 and up, refined with the
// bottom-right alignment pattern.
func DecodeImage(img image.Image, opts *DecodeOptions) (*Result, error) {
	var o DecodeOptions
	if opts != nil {
		o = *opts
	}
	p := newPixels(img)
	lastErr := errors.New("no finder patterns found")
	// A global threshold suits clean images; a local one copes with uneven
	// lighting in photos.
	for _, b := range []*binarizer{p.globalBinarizer(), p.localBinarizer()} {
		triples := b.findFinderTriples()
		for _, t := range triples[:min(3, len(triples))] {
			res, err := b.decodeAt(t, o)
			if err == nil {
				return res, nil
			}
			lastErr = err
		}
	}
	return nil, lastErr
}

// ── pixel access ────────────────────────────────────────────────────────────

type pixels struct {
	w, h int
	rgb  []uint8 // 3 per pixel, composited onto white
	luma []uint8
}

func newPixels(img image.Image) *pixels {
	b := img.Bounds()
	p := &pixels{w: b.Dx(), h: b.Dy()}
	p.rgb = make([]uint8, 3*p.w*p.h)
	p.luma = make([]uint8, p.w*p.h)
	set := func(i int, r, g, bl uint8) {
		p.rgb[3*i], p.rgb[3*i+1], p.rgb[3*i+2] = r, g, bl
		p.luma[i] = uint8((299*int(r) + 587*int(g) + 114*int(bl) + 500) / 1000)
	}
	// Composite onto white so transparent areas count as quiet zone.
	over := func(c, a uint32) uint8 { return uint8((c + 0xffff - a) / 257) }
	switch src := img.(type) {
	case *image.NRGBA:
		for y := range p.h {
			row := src.Pix[(y)*src.Stride:]
			for x := range p.w {
				s := row[4*x : 4*x+4]
				a := uint32(s[3])
				blend := func(c uint8) uint8 { return uint8((uint32(c)*a + 255*(255-a) + 127) / 255) }
				set(y*p.w+x, blend(s[0]), blend(s[1]), blend(s[2]))
			}
		}
	case *image.RGBA:
		for y := range p.h {
			row := src.Pix[(y)*src.Stride:]
			for x := range p.w {
				s := row[4*x : 4*x+4]
				a := 255 - s[3]
				set(y*p.w+x, s[0]+a, s[1]+a, s[2]+a)
			}
		}
	default:
		for y := range p.h {
			for x := range p.w {
				r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				set(y*p.w+x, over(r, a), over(g, a), over(bl, a))
			}
		}
	}
	return p
}

// sample averages the pixels within radius r of (fx, fy).
func (p *pixels) sample(fx, fy, r float64) yuv {
	var sr, sg, sb, n int
	for y := int(math.Floor(fy - r)); y <= int(math.Ceil(fy+r)); y++ {
		for x := int(math.Floor(fx - r)); x <= int(math.Ceil(fx+r)); x++ {
			if !p.inBounds(x, y) {
				continue
			}
			dx, dy := float64(x)+0.5-fx, float64(y)+0.5-fy
			if dx*dx+dy*dy > r*r+0.5 {
				continue
			}
			i := 3 * (y*p.w + x)
			sr += int(p.rgb[i])
			sg += int(p.rgb[i+1])
			sb += int(p.rgb[i+2])
			n++
		}
	}
	if n == 0 {
		return toYUV(255, 255, 255)
	}
	f := float64(n)
	return toYUV(float64(sr)/f, float64(sg)/f, float64(sb)/f)
}

// ── binarization ────────────────────────────────────────────────────────────

// binarizer separates dark from light pixels by luma, either with one
// global (Otsu) threshold or with thresholds that follow local brightness.
type binarizer struct {
	*pixels
	dark   []bool
	global float64
	block  int       // block size of local thresholds; 0 for global
	bw     int       // blocks per row
	thr    []float64 // threshold per block
}

func (p *pixels) globalBinarizer() *binarizer {
	var hist [256]int
	for _, l := range p.luma {
		hist[l]++
	}
	b := &binarizer{pixels: p, global: otsu(hist[:], len(p.luma))}
	b.dark = make([]bool, len(p.luma))
	for i, l := range p.luma {
		b.dark[i] = float64(l) < b.global
	}
	return b
}

// localBinarizer thresholds each block of pixels at the mean of the
// high-contrast blocks around it, like ZXing's hybrid binarizer. Flat areas
// fall back to the global threshold.
func (p *pixels) localBinarizer() *binarizer {
	g := p.globalBinarizer()
	bs := max(4, min(p.w, p.h)/64)
	bw, bh := (p.w+bs-1)/bs, (p.h+bs-1)/bs
	mean := make([]float64, bw*bh)
	edge := make([]bool, bw*bh)
	for by := range bh {
		for bx := range bw {
			lo, hi, sum, n := 255, 0, 0, 0
			for y := by * bs; y < min(p.h, (by+1)*bs); y++ {
				for _, l := range p.luma[y*p.w+bx*bs : y*p.w+min(p.w, (bx+1)*bs)] {
					lo, hi = min(lo, int(l)), max(hi, int(l))
					sum += int(l)
					n++
				}
			}
			mean[by*bw+bx] = float64(sum) / float64(n)
			edge[by*bw+bx] = hi-lo >= 24
		}
	}
	b := &binarizer{pixels: p, global: g.global, block: bs, bw: bw, thr: make([]float64, bw*bh)}
	for by := range bh {
		for bx := range bw {
			sum, n := 0.0, 0
			for y := max(0, by-2); y <= min(bh-1, by+2); y++ {
				for x := max(0, bx-2); x <= min(bw-1, bx+2); x++ {
					if edge[y*bw+x] {
						sum += mean[y*bw+x]
						n++
					}
				}
			}
			b.thr[by*bw+bx] = g.global
			if n > 0 {
				b.thr[by*bw+bx] = sum / float64(n)
			}
		}
	}
	b.dark = make([]bool, len(p.luma))
	for y := range p.h {
		for x := range p.w {
			b.dark[y*p.w+x] = float64(p.luma[y*p.w+x]) < b.thr[y/bs*bw+x/bs]
		}
	}
	return b
}

// threshold returns the luma threshold at pixel (x, y).
func (b *binarizer) threshold(x, y float64) float64 {
	if b.block == 0 {
		return b.global
	}
	ix := min(max(int(x), 0), b.w-1) / b.block
	iy := min(max(int(y), 0), b.h-1) / b.block
	return b.thr[iy*b.bw+ix]
}

func (b *binarizer) isDark(x, y int) bool {
	return b.inBounds(x, y) && b.dark[y*b.w+x]
}

func otsu(hist []int, total int) float64 {
	var sum float64
	for i, n := range hist {
		sum += float64(i * n)
	}
	var sumB, best float64
	var wB int
	th := 128.0
	for i, n := range hist {
		wB += n
		if wB == 0 {
			continue
		}
		wF := total - wB
		if wF == 0 {
			break
		}
		sumB += float64(i * n)
		mB := sumB / float64(wB)
		mF := (sum - sumB) / float64(wF)
		if v := float64(wB) * float64(wF) * (mB - mF) * (mB - mF); v > best {
			best, th = v, float64(i)+0.5
		}
	}
	return th
}

// ── grid sampling ───────────────────────────────────────────────────────────

// grid maps module coordinates of a symbol to image pixels. Module (x, y)
// covers [x, x+1) × [y, y+1); the QR grid spans [0, side).
type grid struct {
	b    *binarizer
	side int
	h    homography
}

// finderGrid maps the three finder centers to their module positions.
func finderGrid(b *binarizer, t finderTriple, side int) (*grid, bool) {
	c := float64(side) - 3.5
	h, ok := affineFrom(
		[3][2]float64{{3.5, 3.5}, {c, 3.5}, {3.5, c}},
		[3][2]float64{{t.tl.x, t.tl.y}, {t.tr.x, t.tr.y}, {t.bl.x, t.bl.y}},
	)
	return &grid{b: b, side: side, h: h}, ok
}

// refine returns a perspective grid through the finder centers and the
// bottom-right alignment pattern, if one is found near where g predicts it.
func (g *grid) refine(t finderTriple) (*grid, bool) {
	if g.side < sideForVersion(2) {
		return nil, false
	}
	a := float64(g.side) - 6.5 // alignment pattern center
	ax, ay := g.h.apply(a, a)
	ux, uy := g.h.apply(a+1, a)
	vx, vy := g.h.apply(a, a+1)
	ux, uy, vx, vy = ux-ax, uy-ay, vx-ax, vy-ay
	m := math.Min(math.Hypot(ux, uy), math.Hypot(vx, vy))
	if m < 1 {
		return nil, false
	}
	// Score a candidate center by how well the 5×5 pattern (dark center,
	// light ring, dark ring) matches the local threshold. Perspective
	// changes the module size near the pattern, so the template is scaled
	// by k.
	score := func(cx, cy, k float64) int {
		hits := 0
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				px := cx + k*(float64(dx)*ux+float64(dy)*vx)
				py := cy + k*(float64(dx)*uy+float64(dy)*vy)
				want := max(abs(dx), abs(dy)) != 1
				if g.b.isDark(int(math.Floor(px)), int(math.Floor(py))) == want {
					hits++
				}
			}
		}
		return hits
	}
	best, bx, by, bk := -1, ax, ay, 1.0
	search := func(cx, cy, radius, step float64, scales []float64) {
		for _, k := range scales {
			for oy := -radius; oy <= radius; oy += step {
				for ox := -radius; ox <= radius; ox += step {
					// Prefer the candidate nearest the prediction on ties.
					s := score(cx+ox, cy+oy, k)
					if s > best || (s == best && math.Hypot(cx+ox-ax, cy+oy-ay) < math.Hypot(bx-ax, by-ay)) {
						best, bx, by, bk = s, cx+ox, cy+oy, k
					}
				}
			}
		}
	}
	// Perspective moves the pattern away from the affine prediction, the
	// more so the larger the symbol.
	search(ax, ay, math.Max(4, float64(g.side)/5)*m, math.Max(1, m/3), []float64{0.6, 0.7, 0.8, 0.9, 1, 1.1, 1.25, 1.4})
	search(bx, by, bk*m/2, math.Max(0.5, bk*m/8), []float64{bk})
	if best < 24 {
		return nil, false
	}
	// Center the match: average the positions that score as well.
	var sx, sy, n float64
	step := math.Max(0.5, bk*m/8)
	for oy := -bk * m / 2; oy <= bk*m/2; oy += step {
		for ox := -bk * m / 2; ox <= bk*m/2; ox += step {
			if score(bx+ox, by+oy, bk) == best {
				sx += bx + ox
				sy += by + oy
				n++
			}
		}
	}
	bx, by = sx/n, sy/n

	c := float64(g.side) - 3.5
	h, ok := homographyFrom(
		[4][2]float64{{3.5, 3.5}, {c, 3.5}, {3.5, c}, {a, a}},
		[4][2]float64{{t.tl.x, t.tl.y}, {t.tr.x, t.tr.y}, {t.bl.x, t.bl.y}, {bx, by}},
	)
	if !ok {
		return nil, false
	}
	return &grid{b: g.b, side: g.side, h: h}, true
}

// at samples the color of module q.
func (g *grid) at(q point) yuv {
	mx, my := float64(q.x)+0.5, float64(q.y)+0.5
	px, py := g.h.apply(mx, my)
	return g.b.sample(px, py, 0.3*g.h.moduleScale(mx, my))
}

// dark reports whether module q is dark by the local threshold.
func (g *grid) dark(q point) bool {
	px, py := g.h.apply(float64(q.x)+0.5, float64(q.y)+0.5)
	return g.at(q).Y < g.b.threshold(px, py)
}

func (g *grid) corners() [4]Point {
	var c [4]Point
	n := float64(g.side + 1)
	for i, q := range [4][2]float64{{-1, -1}, {n, -1}, {n, n}, {-1, n}} {
		c[i].X, c[i].Y = g.h.apply(q[0], q[1])
	}
	return c
}

// ── symbol decoding ─────────────────────────────────────────────────────────

func (b *binarizer) decodeAt(t finderTriple, o DecodeOptions) (*Result, error) {
	m := (t.tl.module + t.tr.module + t.bl.module) / 3
	est := ((dist(t.tl, t.tr)+dist(t.tl, t.bl))/2/m + 7 - 17) / 4
	v0 := int(math.Round(est))

	// The estimate drifts for large or tilted symbols, so try its neighbors
	// too; the format, version information and RS checks reject wrong
	// guesses.
	candidates := []int{v0}
	for d := 1; d <= 3; d++ {
		candidates = append(candidates, v0+d, v0-d)
	}
	lastErr := errors.New("no plausible version")
	for _, v := range candidates {
		if v < MinVersion || v > MaxVersion {
			continue
		}
		g, ok := finderGrid(b, t, sideForVersion(v))
		if !ok {
			continue
		}
		grids := []*grid{g}
		if pg, ok := g.refine(t); ok {
			grids = []*grid{pg, g}
		}
		for _, g := range grids {
			res, err := g.decode(v, o)
			if err == nil {
				return res, nil
			}
			lastErr = err
		}
	}
	return nil, lastErr
}

func (g *grid) decode(v int, o DecodeOptions) (*Result, error) {
	side := g.side
	if v >= 7 {
		best := 99
		for _, pos := range versionPositions(side) {
			var raw uint32
			for i, q := range pos {
				if g.dark(q) {
					raw |= 1 << i
				}
			}
			if dv, d := decodeVersion(raw); dv == v && d < best {
				best = d
			}
		}
		if best > 3 {
			return nil, fmt.Errorf("version %d: version information mismatch", v)
		}
	}

	var raws [2]uint32
	for c, pos := range formatPositions(side) {
		for i, q := range pos {
			if g.dark(q) {
				raws[c] |= 1 << i
			}
		}
	}
	fi, d := decodeFormat(raws[0], raws[1])
	if d > 3 {
		return nil, fmt.Errorf("version %d: unreadable format information", v)
	}

	var lastErr error
	for _, s := range g.schemeOrder() {
		res, err := g.decodeData(v, fi, s, o)
		if err == nil {
			return res, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// schemeOrder guesses the color scheme from the palette patterns. Read as
// 8-color strips, a 4-color symbol shows each color twice: cells k and k+4
// match. The likelier scheme comes first; Reed-Solomon has the final say.
func (g *grid) schemeOrder() []Scheme {
	strips := paletteStrips(g.side, 8)
	if len(strips[0]) == 0 {
		return []Scheme{FourColor}
	}
	var means [8]yuv
	var counts [8]float64
	for _, strip := range strips {
		for k, q := range strip {
			c := g.at(q)
			means[k%8].Y += c.Y
			means[k%8].U += c.U
			means[k%8].V += c.V
			counts[k%8]++
		}
	}
	for i := range means {
		means[i] = yuv{means[i].Y / counts[i], means[i].U / counts[i], means[i].V / counts[i]}
	}
	var same, all float64
	for i := range 4 {
		same += math.Sqrt(means[i].dist2(means[i+4])) / 4
	}
	for i := range 8 {
		for j := i + 1; j < 8; j++ {
			all += math.Sqrt(means[i].dist2(means[j])) / 28
		}
	}
	if same < 0.4*all {
		return []Scheme{FourColor, EightColor}
	}
	return []Scheme{EightColor, FourColor}
}

// decodeData reads the data region of a version v symbol with format fi and
// color scheme s.
func (g *grid) decodeData(v int, fi formatInfo, s Scheme, o DecodeOptions) (*Result, error) {
	l, err := newLayout(v, s)
	if err != nil {
		return nil, err
	}

	// Learn the palette from the four palette patterns.
	refs := make([]yuv, len(l.palette))
	for i, pts := range l.palette {
		for _, q := range pts {
			c := g.at(q)
			refs[i].Y += c.Y
			refs[i].U += c.U
			refs[i].V += c.V
		}
		n := float64(len(pts))
		refs[i] = yuv{refs[i].Y / n, refs[i].U / n, refs[i].V / n}
	}

	samples := make([]yuv, len(l.data))
	for i, q := range l.data {
		samples[i] = g.at(q)
	}
	var labels []int
	switch o.Classifier {
	case MinDistance:
		labels = classifyNearest(samples, refs)
	default:
		labels = classifyKMeans(samples, refs)
	}

	bpm := s.BitsPerModule()
	full := int(s) - 1
	var bw bitWriter
	for i, q := range l.data {
		val := labels[i]
		if maskBit(fi.mask, q.x, q.y) {
			val ^= full
		}
		bw.write(uint32(val), bpm)
	}
	blocks := newBlockLayout(v, fi.level, s)
	data, corrected, err := blocks.deinterleave(bw.bytes()[:blocks.total()])
	if err != nil {
		return nil, fmt.Errorf("version %d %s: %w", v, s, err)
	}
	out, err := readSegments(data, v, s)
	if err != nil {
		return nil, fmt.Errorf("version %d %s: %w", v, s, err)
	}
	return &Result{
		Data:      out,
		Version:   v,
		Scheme:    s,
		Level:     fi.level,
		Mask:      fi.mask,
		Corrected: corrected,
		Corners:   g.corners(),
	}, nil
}
