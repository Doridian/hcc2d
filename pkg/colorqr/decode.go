package colorqr

import (
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // register decoders for Decode
	_ "image/jpeg" //
	_ "image/png"  //
	"io"
	"math"
	"sort"
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

// Result is a decoded symbol.
type Result struct {
	Data      []byte
	Version   int
	Scheme    Scheme
	Level     ECLevel
	Mask      int
	Corrected int // bytes fixed by Reed-Solomon
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

// DecodeImage locates and decodes a symbol in img. The symbol may be scaled
// and rotated; the module grid is derived from the three finder patterns by
// an affine transform, so strong perspective distortion is not handled.
func DecodeImage(img image.Image, opts *DecodeOptions) (*Result, error) {
	var o DecodeOptions
	if opts != nil {
		o = *opts
	}
	p := newPixels(img)
	triples := p.findFinderTriples()
	if len(triples) == 0 {
		return nil, errors.New("no finder patterns found")
	}
	var lastErr error
	for _, t := range triples[:min(3, len(triples))] {
		res, err := p.decodeAt(t, o)
		if err == nil {
			return res, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// ── pixel access ────────────────────────────────────────────────────────────

type pixels struct {
	w, h      int
	rgb       []float64 // 3 per pixel
	dark      []bool
	threshold float64
}

func newPixels(img image.Image) *pixels {
	b := img.Bounds()
	p := &pixels{w: b.Dx(), h: b.Dy()}
	p.rgb = make([]float64, 3*p.w*p.h)
	luma := make([]float64, p.w*p.h)
	var hist [256]int
	for y := 0; y < p.h; y++ {
		for x := 0; x < p.w; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			// Composite onto white so transparent areas count as quiet zone.
			fr := (float64(r) + float64(0xffff-a)) / 257
			fg := (float64(g) + float64(0xffff-a)) / 257
			fb := (float64(bl) + float64(0xffff-a)) / 257
			i := y*p.w + x
			p.rgb[3*i], p.rgb[3*i+1], p.rgb[3*i+2] = fr, fg, fb
			luma[i] = 0.299*fr + 0.587*fg + 0.114*fb
			hist[min(255, int(luma[i]))]++
		}
	}
	p.threshold = otsu(hist[:], p.w*p.h)
	p.dark = make([]bool, p.w*p.h)
	for i, l := range luma {
		p.dark[i] = l < p.threshold
	}
	return p
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

func (p *pixels) isDark(x, y int) bool {
	if x < 0 || y < 0 || x >= p.w || y >= p.h {
		return false
	}
	return p.dark[y*p.w+x]
}

// sample averages the pixels within radius r of (fx, fy).
func (p *pixels) sample(fx, fy, r float64) (yuv, bool) {
	var sr, sg, sb float64
	n := 0
	for y := int(math.Floor(fy - r)); y <= int(math.Ceil(fy+r)); y++ {
		for x := int(math.Floor(fx - r)); x <= int(math.Ceil(fx+r)); x++ {
			if x < 0 || y < 0 || x >= p.w || y >= p.h {
				continue
			}
			dx, dy := float64(x)+0.5-fx, float64(y)+0.5-fy
			if dx*dx+dy*dy > r*r+0.5 {
				continue
			}
			i := 3 * (y*p.w + x)
			sr += p.rgb[i]
			sg += p.rgb[i+1]
			sb += p.rgb[i+2]
			n++
		}
	}
	if n == 0 {
		return yuv{}, false
	}
	return toYUV(sr/float64(n), sg/float64(n), sb/float64(n)), true
}

// ── finder pattern detection ────────────────────────────────────────────────

type finder struct {
	x, y   float64
	module float64
	count  int
}

// ratioOK checks runs dark:light:dark:light:dark against 1:1:3:1:1.
func ratioOK(r [5]int) (float64, bool) {
	total := 0
	for _, n := range r {
		if n == 0 {
			return 0, false
		}
		total += n
	}
	if total < 7 {
		return 0, false
	}
	m := float64(total) / 7
	v := m / 2
	ok := math.Abs(m-float64(r[0])) < v && math.Abs(m-float64(r[1])) < v &&
		math.Abs(3*m-float64(r[2])) < 3*v &&
		math.Abs(m-float64(r[3])) < v && math.Abs(m-float64(r[4])) < v
	return m, ok
}

// crossCheck walks from (x, y) along ±(dx, dy) and verifies a finder
// pattern centered there. It returns the refined center offset (in steps
// along the direction) and the module size in steps.
func (p *pixels) crossCheck(x, y, dx, dy, maxRun int) (float64, float64, bool) {
	if !p.isDark(x, y) {
		return 0, 0, false
	}
	var r [5]int
	// Backwards: rest of center, light, dark.
	i := 0
	for p.isDark(x-i*dx, y-i*dy) {
		r[2]++
		i++
	}
	for !p.isDark(x-i*dx, y-i*dy) && r[1] <= maxRun {
		if !p.inBounds(x-i*dx, y-i*dy) {
			return 0, 0, false
		}
		r[1]++
		i++
	}
	for p.isDark(x-i*dx, y-i*dy) && r[0] <= maxRun {
		r[0]++
		i++
	}
	back := r[2]
	// Forwards.
	i = 1
	for p.isDark(x+i*dx, y+i*dy) {
		r[2]++
		i++
	}
	fwd := r[2] - back
	for !p.isDark(x+i*dx, y+i*dy) && r[3] <= maxRun {
		if !p.inBounds(x+i*dx, y+i*dy) {
			return 0, 0, false
		}
		r[3]++
		i++
	}
	for p.isDark(x+i*dx, y+i*dy) && r[4] <= maxRun {
		r[4]++
		i++
	}
	m, ok := ratioOK(r)
	if !ok {
		return 0, 0, false
	}
	// Center of the middle run relative to (x, y).
	return float64(fwd-back+1) / 2, m, true
}

func (p *pixels) inBounds(x, y int) bool { return x >= 0 && y >= 0 && x < p.w && y < p.h }

func (p *pixels) findFinders() []finder {
	var found []finder
	add := func(f finder) {
		for i := range found {
			g := &found[i]
			if math.Hypot(g.x-f.x, g.y-f.y) < 2*g.module && math.Abs(g.module-f.module) < 0.5*g.module+1 {
				n := float64(g.count)
				g.x = (g.x*n + f.x) / (n + 1)
				g.y = (g.y*n + f.y) / (n + 1)
				g.module = (g.module*n + f.module) / (n + 1)
				g.count++
				return
			}
		}
		f.count = 1
		found = append(found, f)
	}

	for y := 0; y < p.h; y++ {
		// Run-length encode the row.
		var starts, lens []int
		var colors []bool
		for x := 0; x < p.w; x++ {
			d := p.isDark(x, y)
			if len(colors) > 0 && colors[len(colors)-1] == d {
				lens[len(lens)-1]++
				continue
			}
			starts = append(starts, x)
			lens = append(lens, 1)
			colors = append(colors, d)
		}
		for i := 0; i+5 <= len(lens); i++ {
			if !colors[i] {
				continue
			}
			r := [5]int{lens[i], lens[i+1], lens[i+2], lens[i+3], lens[i+4]}
			mh, ok := ratioOK(r)
			if !ok {
				continue
			}
			maxRun := int(4*mh) + 2
			cx := starts[i+2] + lens[i+2]/2
			offY, mv, ok := p.crossCheck(cx, y, 0, 1, maxRun)
			if !ok || math.Abs(mv-mh) > 0.5*mh {
				continue
			}
			cy := int(math.Round(float64(y) + offY))
			offX, mh2, ok := p.crossCheck(cx, cy, 1, 0, maxRun)
			if !ok {
				continue
			}
			fx := float64(cx) + offX
			if _, _, ok := p.crossCheck(int(math.Round(fx)), cy, 1, 1, maxRun); !ok {
				continue
			}
			add(finder{x: fx + 0.5, y: float64(y) + offY + 0.5, module: (mv + mh2) / 2})
		}
	}
	return found
}

type finderTriple struct {
	tl, tr, bl finder
	score      float64
}

// findFinderTriples returns plausible (top-left, top-right, bottom-left)
// finder combinations, best first.
func (p *pixels) findFinderTriples() []finderTriple {
	fs := p.findFinders()
	sort.Slice(fs, func(i, j int) bool { return fs[i].count > fs[j].count })
	fs = fs[:min(len(fs), 12)]

	var out []finderTriple
	for i := range fs {
		for j := i + 1; j < len(fs); j++ {
			for k := j + 1; k < len(fs); k++ {
				if t, ok := makeTriple(fs[i], fs[j], fs[k]); ok {
					out = append(out, t)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].score > out[j].score })
	return out
}

func makeTriple(a, b, c finder) (finderTriple, bool) {
	ms := []float64{a.module, b.module, c.module}
	sort.Float64s(ms)
	if ms[2] > 1.5*ms[0] {
		return finderTriple{}, false
	}
	// The top-left finder is opposite the longest side.
	dab, dbc, dca := dist(a, b), dist(b, c), dist(c, a)
	tl, p, q := a, b, c
	legA, legB, hyp := dab, dca, dbc
	switch {
	case dab >= dbc && dab >= dca:
		tl, p, q = c, a, b
		legA, legB, hyp = dbc, dca, dab
	case dca >= dab && dca >= dbc:
		tl, p, q = b, c, a
		legA, legB, hyp = dab, dbc, dca
	}
	m := (ms[0] + ms[1] + ms[2]) / 3
	if legA < 14*m || legB < 14*m { // side of at least 21 modules
		return finderTriple{}, false
	}
	legErr := math.Abs(legA-legB) / math.Max(legA, legB)
	hypErr := math.Abs(hyp*hyp-legA*legA-legB*legB) / (hyp * hyp)
	if legErr > 0.2 || hypErr > 0.2 {
		return finderTriple{}, false
	}
	// Orientation: in image coordinates (y down) the cross product of
	// TR-TL and BL-TL is positive.
	if (p.x-tl.x)*(q.y-tl.y)-(p.y-tl.y)*(q.x-tl.x) < 0 {
		p, q = q, p
	}
	score := float64(a.count+b.count+c.count) * (1 - legErr - hypErr)
	return finderTriple{tl: tl, tr: p, bl: q, score: score}, true
}

func dist(a, b finder) float64 { return math.Hypot(a.x-b.x, a.y-b.y) }

// ── grid sampling ───────────────────────────────────────────────────────────

type grid struct {
	p              *pixels
	side           int
	ox, oy         float64 // pixel position of module coordinate (0, 0)
	ux, uy, vx, vy float64 // pixel step per module along x and y
	radius         float64
}

func newGrid(p *pixels, t finderTriple, side int) *grid {
	n := float64(side - 7) // finder centers are side-7 modules apart
	g := &grid{p: p, side: side}
	g.ux, g.uy = (t.tr.x-t.tl.x)/n, (t.tr.y-t.tl.y)/n
	g.vx, g.vy = (t.bl.x-t.tl.x)/n, (t.bl.y-t.tl.y)/n
	g.ox = t.tl.x - 3.5*(g.ux+g.vx)
	g.oy = t.tl.y - 3.5*(g.uy+g.vy)
	g.radius = 0.3 * math.Min(math.Hypot(g.ux, g.uy), math.Hypot(g.vx, g.vy))
	return g
}

func (g *grid) at(q point) yuv {
	mx, my := float64(q.x)+0.5, float64(q.y)+0.5
	c, _ := g.p.sample(g.ox+mx*g.ux+my*g.vx, g.oy+mx*g.uy+my*g.vy, g.radius)
	return c
}

func (g *grid) dark(q point) bool { return g.at(q).Y < g.p.threshold }

func (p *pixels) decodeAt(t finderTriple, o DecodeOptions) (*Result, error) {
	m := (t.tl.module + t.tr.module + t.bl.module) / 3
	est := ((dist(t.tl, t.tr)+dist(t.tl, t.bl))/2/m + 7 - 17) / 4
	v0 := int(math.Round(est))

	// The estimate drifts for large symbols, so try its neighbors too; the
	// format, version information and RS checks reject wrong guesses.
	candidates := []int{v0}
	for d := 1; d <= 3; d++ {
		candidates = append(candidates, v0+d, v0-d)
	}
	lastErr := errors.New("no plausible version")
	for _, v := range candidates {
		if v < MinVersion || v > MaxVersion {
			continue
		}
		res, err := p.decodeVersion(t, v, o)
		if err == nil {
			return res, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (p *pixels) decodeVersion(t finderTriple, v int, o DecodeOptions) (*Result, error) {
	side := sideForVersion(v)
	g := newGrid(p, t, side)

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

	var fi formatInfo
	best := 99
	for _, pos := range formatPositions(side) {
		var raw uint32
		for i, q := range pos {
			if g.dark(q) {
				raw |= 1 << i
			}
		}
		if f, d := decodeFormat(raw); d < best {
			fi, best = f, d
		}
	}
	if best > 3 {
		return nil, fmt.Errorf("version %d: unreadable format information", v)
	}

	l, err := newLayout(v, fi.scheme)
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

	bpm := fi.scheme.BitsPerModule()
	full := int(fi.scheme) - 1
	var bw bitWriter
	for i, q := range l.data {
		val := labels[i]
		if maskBit(fi.mask, q.x, q.y) {
			val ^= full
		}
		bw.write(uint32(val), bpm)
	}
	cw := bw.bytes()[:l.codewords()]

	blocks, err := newBlockLayout(len(cw), fi.level)
	if err != nil {
		return nil, err
	}
	data, corrected, err := blocks.deinterleave(cw)
	if err != nil {
		return nil, fmt.Errorf("version %d: %w", v, err)
	}

	br := bitReader{buf: data}
	if mode := br.read(4); mode != modeByte {
		return nil, fmt.Errorf("unsupported segment mode %04b", mode)
	}
	n := int(br.read(countBits))
	if 4+countBits+8*n > 8*len(data) {
		return nil, fmt.Errorf("declared length %d exceeds symbol capacity", n)
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(br.read(8))
	}
	return &Result{
		Data:      out,
		Version:   v,
		Scheme:    fi.scheme,
		Level:     fi.level,
		Mask:      fi.mask,
		Corrected: corrected,
	}, nil
}

// ── color classification ────────────────────────────────────────────────────

func nearest(c yuv, refs []yuv) int {
	best, bestD := 0, math.Inf(1)
	for i, r := range refs {
		if d := c.dist2(r); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

func classifyNearest(samples, refs []yuv) []int {
	labels := make([]int, len(samples))
	for i, s := range samples {
		labels[i] = nearest(s, refs)
	}
	return labels
}

// classifyKMeans runs Lloyd's algorithm with k = palette size, starting from
// the palette-pattern colors so that cluster i stays associated with
// palette entry i.
func classifyKMeans(samples, refs []yuv) []int {
	cent := append([]yuv(nil), refs...)
	labels := classifyNearest(samples, cent)
	for range 30 {
		sums := make([]yuv, len(cent))
		counts := make([]int, len(cent))
		for i, s := range samples {
			k := labels[i]
			sums[k].Y += s.Y
			sums[k].U += s.U
			sums[k].V += s.V
			counts[k]++
		}
		for k := range cent {
			if counts[k] > 0 {
				n := float64(counts[k])
				cent[k] = yuv{sums[k].Y / n, sums[k].U / n, sums[k].V / n}
			}
		}
		next := classifyNearest(samples, cent)
		changed := false
		for i := range next {
			if next[i] != labels[i] {
				changed = true
				break
			}
		}
		labels = next
		if !changed {
			break
		}
	}
	return labels
}
