package colorqr

import "fmt"

// point is a module coordinate: x is the column, y the row.
type point struct{ x, y int }

// layout is the module map shared by the encoder and the decoder for a given
// version and color scheme.
//
// HCC2D keeps every QR function pattern (finder patterns, separators, timing
// patterns, alignment patterns, format and version information, dark module)
// in black and white and in its QR position, so the data modules are exactly
// QR's. It adds four Color Palette Patterns that the decoder uses to learn
// how each color looks after printing and scanning. As in Figure 3 of
// Querini and Italiano (FedCSIS 2013), they are one module thick and lie in
// the ring just outside the QR grid (rows -1 and side, columns -1 and side),
// so together they take "only 2 rows and 2 columns". Each repeats the palette
// (cell k shows color k mod n) for as many whole cycles as fit, starting next
// to the separator of a finder pattern:
//
//   - top:    row -1, rightwards from column 8 (top-left finder)
//   - left:   column -1, upwards from row side-9 (bottom-left finder)
//   - bottom: row side, rightwards from column 8 (bottom-left finder)
//   - right:  column side, downwards from row 8 (top-right finder)
//
// The top and left strips end at the separator of the opposite finder; the
// bottom and right strips run to the far corner.
type layout struct {
	version int
	side    int
	scheme  Scheme

	function [][]bool  // true for every non-data module
	fixed    [][]bool  // dark modules of the static function patterns
	palette  [][]point // palette pattern cells of each color, in the ring
	data     []point   // data modules in placement order
}

func newLayout(version int, scheme Scheme) (*layout, error) {
	if version < MinVersion || version > MaxVersion {
		return nil, fmt.Errorf("version %d out of range", version)
	}
	side := sideForVersion(version)
	l := &layout{
		version:  version,
		side:     side,
		scheme:   scheme,
		function: newBoolGrid(side),
		fixed:    newBoolGrid(side),
	}

	// Timing patterns (drawn first; finders overwrite their ends).
	for i := range side {
		l.setFunction(6, i, i%2 == 0)
		l.setFunction(i, 6, i%2 == 0)
	}

	// Finder patterns with their separators.
	for _, c := range []point{{3, 3}, {side - 4, 3}, {3, side - 4}} {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				x, y := c.x+dx, c.y+dy
				if x < 0 || y < 0 || x >= side || y >= side {
					continue
				}
				d := max(abs(dx), abs(dy))
				l.setFunction(x, y, d != 2 && d != 4)
			}
		}
	}

	// Alignment patterns, skipping the three that would overlap finders.
	pos := alignmentPositions[version]
	for i, ax := range pos {
		for j, ay := range pos {
			if (i == 0 && j == 0) || (i == 0 && j == len(pos)-1) || (i == len(pos)-1 && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					l.setFunction(ax+dx, ay+dy, max(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}

	// Format information areas and the dark module.
	for _, p := range formatPositions(side) {
		for _, q := range p {
			l.function[q.y][q.x] = true
		}
	}
	l.setFunction(8, side-8, true)

	// Version information areas.
	if version >= 7 {
		for _, p := range versionPositions(side) {
			for _, q := range p {
				l.function[q.y][q.x] = true
			}
		}
	}

	// Color Palette Patterns.
	strips := paletteStrips(side, int(scheme))
	if len(strips[0]) == 0 {
		return nil, fmt.Errorf("version %d is too small for the %s palette patterns", version, scheme)
	}
	l.palette = make([][]point, int(scheme))
	for _, strip := range strips {
		for k, p := range strip {
			l.palette[k%int(scheme)] = append(l.palette[k%int(scheme)], p)
		}
	}

	// Data modules in QR placement order: two-column strips from the right,
	// alternating upwards and downwards, skipping the vertical timing column.
	for right := side - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		upward := (right+1)&2 == 0
		for vert := range side {
			y := vert
			if upward {
				y = side - 1 - vert
			}
			for j := range 2 {
				x := right - j
				if !l.function[y][x] {
					l.data = append(l.data, point{x, y})
				}
			}
		}
	}
	return l, nil
}

// paletteStrips returns the cells of the four Color Palette Patterns of an
// n-color symbol, in order (cell k shows color k mod n).
func paletteStrips(side, n int) [4][]point {
	short := (side - 16) / n * n // between two finders: columns 8 … side-9
	long := (side - 8) / n * n   // from a finder to the far corner: 8 … side-1
	var s [4][]point
	for k := range short {
		s[0] = append(s[0], point{8 + k, -1})        // top
		s[1] = append(s[1], point{-1, side - 9 - k}) // left
	}
	for k := range long {
		s[2] = append(s[2], point{8 + k, side}) // bottom
		s[3] = append(s[3], point{side, 8 + k}) // right
	}
	return s
}

func (l *layout) setFunction(x, y int, dark bool) {
	l.function[y][x] = true
	l.fixed[y][x] = dark
}

// formatPositions returns the two copies of the 15 format bits, bit i at
// index i, exactly where QR puts them.
func formatPositions(side int) [2][15]point {
	var p [2][15]point
	for i := range 6 {
		p[0][i] = point{8, i}
	}
	p[0][6] = point{8, 7}
	p[0][7] = point{8, 8}
	p[0][8] = point{7, 8}
	for i := 9; i < 15; i++ {
		p[0][i] = point{14 - i, 8}
	}
	for i := range 8 {
		p[1][i] = point{side - 1 - i, 8}
	}
	for i := 8; i < 15; i++ {
		p[1][i] = point{8, side - 15 + i}
	}
	return p
}

// versionPositions returns the two copies of the 18 version bits.
func versionPositions(side int) [2][18]point {
	var p [2][18]point
	for i := range 18 {
		a, b := side-11+i%3, i/3
		p[0][i] = point{a, b} // top right
		p[1][i] = point{b, a} // bottom left
	}
	return p
}

// maskBit reports whether data mask m inverts module (x, y). These are the
// eight QR mask patterns; inverting a colored module means complementing
// its value, which swaps each color with its opposite.
func maskBit(m, x, y int) bool {
	switch m {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (x/3+y/2)%2 == 0
	case 5:
		return x*y%2+x*y%3 == 0
	case 6:
		return (x*y%2+x*y%3)%2 == 0
	case 7:
		return ((x+y)%2+x*y%3)%2 == 0
	}
	panic("invalid mask")
}

func newBoolGrid(n int) [][]bool {
	g := make([][]bool, n)
	for i := range g {
		g[i] = make([]bool, n)
	}
	return g
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
