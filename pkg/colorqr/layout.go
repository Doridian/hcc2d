package colorqr

import "fmt"

// point is a module coordinate: x is the column, y the row.
type point struct{ x, y int }

// layout is the module map shared by the encoder and the decoder for a given
// version and color scheme.
//
// HCC2D keeps every QR function pattern (finder patterns, separators, timing
// patterns, alignment patterns, format and version information, dark module)
// in black and white, and adds four Color Palette Patterns that the decoder
// uses to learn how each color looks after printing and scanning. Each
// palette pattern is a strip two modules thick in which module i shows
// palette color i; the strips sit at the middle of the four edges, away
// from the finder patterns and from each other.
type layout struct {
	version int
	side    int
	scheme  Scheme

	function [][]bool // true for every non-data module
	fixed    [][]bool // dark modules of the static function patterns
	palette  [][]point
	data     []point // data modules in placement order
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
	n := int(scheme)
	hi := side - 8 // first column of the top-right separator
	if version >= 7 {
		hi = side - 11 // first column of the top-right version block
	}
	if hi-9 < n {
		return nil, fmt.Errorf("version %d is too small for the %s palette patterns", version, scheme)
	}
	c0 := 9 + (hi-9-n)/2
	l.palette = make([][]point, n)
	for i := range n {
		c := c0 + i
		for _, p := range []point{
			{c, 0}, {c, 1}, // top
			{c, side - 2}, {c, side - 1}, // bottom
			{0, c}, {1, c}, // left
			{side - 2, c}, {side - 1, c}, // right
		} {
			if l.function[p.y][p.x] {
				return nil, fmt.Errorf("palette pattern collides with a function pattern at (%d,%d)", p.x, p.y)
			}
			l.function[p.y][p.x] = true
			l.palette[i] = append(l.palette[i], p)
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

func (l *layout) setFunction(x, y int, dark bool) {
	l.function[y][x] = true
	l.fixed[y][x] = dark
}

// codewords is the number of whole bytes the data modules can hold.
func (l *layout) codewords() int {
	return len(l.data) * l.scheme.BitsPerModule() / 8
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
