package colorqr

import (
	"math"
	"sort"
)

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
func (p *binarizer) crossCheck(x, y, dx, dy, maxRun int) (float64, float64, bool) {
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

func (p *binarizer) findFinders() []finder {
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
func (p *binarizer) findFinderTriples() []finderTriple {
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
