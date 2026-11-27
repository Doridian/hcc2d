package colorqr

import "math"

// homography maps module coordinates to pixel coordinates:
// (x, y) → ((a·x + b·y + c) / (g·x + h·y + 1), (d·x + e·y + f) / (g·x + h·y + 1)).
type homography [8]float64

func (h homography) apply(x, y float64) (float64, float64) {
	w := h[6]*x + h[7]*y + 1
	return (h[0]*x + h[1]*y + h[2]) / w, (h[3]*x + h[4]*y + h[5]) / w
}

// affineFrom returns the affine map through three point pairs.
func affineFrom(src, dst [3][2]float64) (homography, bool) {
	// Solve for a, b, c and d, e, f separately.
	var m [3][4]float64
	for i := range 3 {
		m[i] = [4]float64{src[i][0], src[i][1], 1, dst[i][0]}
	}
	abc, ok := solve3(m)
	if !ok {
		return homography{}, false
	}
	for i := range 3 {
		m[i] = [4]float64{src[i][0], src[i][1], 1, dst[i][1]}
	}
	def, ok := solve3(m)
	if !ok {
		return homography{}, false
	}
	return homography{abc[0], abc[1], abc[2], def[0], def[1], def[2], 0, 0}, true
}

func solve3(m [3][4]float64) ([3]float64, bool) {
	rows := make([][]float64, 3)
	for i := range rows {
		rows[i] = m[i][:]
	}
	x, ok := gaussSolve(rows)
	if !ok {
		return [3]float64{}, false
	}
	return [3]float64{x[0], x[1], x[2]}, true
}

// homographyFrom returns the projective map through four point pairs.
func homographyFrom(src, dst [4][2]float64) (homography, bool) {
	rows := make([][]float64, 8)
	for i := range 4 {
		x, y, u, v := src[i][0], src[i][1], dst[i][0], dst[i][1]
		rows[2*i] = []float64{x, y, 1, 0, 0, 0, -u * x, -u * y, u}
		rows[2*i+1] = []float64{0, 0, 0, x, y, 1, -v * x, -v * y, v}
	}
	sol, ok := gaussSolve(rows)
	if !ok {
		return homography{}, false
	}
	var h homography
	copy(h[:], sol)
	return h, true
}

// gaussSolve solves the augmented n×(n+1) system in place with partial
// pivoting.
func gaussSolve(m [][]float64) ([]float64, bool) {
	n := len(m)
	for c := range n {
		p := c
		for r := c + 1; r < n; r++ {
			if math.Abs(m[r][c]) > math.Abs(m[p][c]) {
				p = r
			}
		}
		if math.Abs(m[p][c]) < 1e-12 {
			return nil, false
		}
		m[c], m[p] = m[p], m[c]
		for r := range n {
			if r == c {
				continue
			}
			f := m[r][c] / m[c][c]
			for k := c; k <= n; k++ {
				m[r][k] -= f * m[c][k]
			}
		}
	}
	x := make([]float64, n)
	for i := range n {
		x[i] = m[i][n] / m[i][i]
	}
	return x, true
}

// moduleScale returns the approximate pixel size of a module at module
// coordinate (x, y).
func (h homography) moduleScale(x, y float64) float64 {
	x0, y0 := h.apply(x, y)
	x1, y1 := h.apply(x+1, y)
	x2, y2 := h.apply(x, y+1)
	return math.Min(math.Hypot(x1-x0, y1-y0), math.Hypot(x2-x0, y2-y0))
}
