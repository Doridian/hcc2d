package colorqr

// cellKind classifies what purpose a cell at (row, col) serves within the
// inner grid (i.e. after stripping the quiet zone).
type cellKind int

const (
	kindData    cellKind = iota
	kindFinder           // finder pattern cell
	kindSep              // separator (white border around finder)
	kindTiming           // timing pattern
	kindFormat           // format info (version / scheme)
	kindPalette          // color palette strip
)

// layout pre-computes the role of every cell for a given inner grid side.
type layout struct {
	side int
	grid [][]cellKind
}

func newLayout(side int) *layout {
	g := make([][]cellKind, side)
	for r := range g {
		g[r] = make([]cellKind, side)
	}
	l := &layout{side: side, grid: g}
	l.mark()
	return l
}

func (l *layout) mark() {
	s := l.side

	// ── finder patterns (top-left, top-right, bottom-left) ────────────────
	for _, origin := range finderOrigins(s) {
		for dr := 0; dr < finderSize; dr++ {
			for dc := 0; dc < finderSize; dc++ {
				l.set(origin[0]+dr, origin[1]+dc, kindFinder)
			}
		}
		// separator ring
		for dr := -1; dr <= finderSize; dr++ {
			l.set(origin[0]+dr, origin[1]-1, kindSep)
			l.set(origin[0]+dr, origin[1]+finderSize, kindSep)
		}
		for dc := -1; dc <= finderSize; dc++ {
			l.set(origin[0]-1, origin[1]+dc, kindSep)
			l.set(origin[0]+finderSize, origin[1]+dc, kindSep)
		}
	}

	// ── timing patterns (row 6 and col 6 in QR; we use finderSize-1) ──────
	timingRow := finderSize - 1
	timingCol := finderSize - 1
	for i := timingOffset; i < s-timingOffset; i++ {
		l.set(timingRow, i, kindTiming)
		l.set(i, timingCol, kindTiming)
	}

	// ── format info row & col (row 0 top strip) ───────────────────────────
	for i := 0; i < s; i++ {
		if l.grid[0][i] == kindData {
			l.set(0, i, kindFormat)
		}
		if l.grid[i][0] == kindData {
			l.set(i, 0, kindFormat)
		}
	}

	// ── palette strips (2 cells wide, along bottom and right edges) ────────
	for i := 0; i < s; i++ {
		for d := 0; d < paletteStrip; d++ {
			l.set(s-1-d, i, kindPalette)
			l.set(i, s-1-d, kindPalette)
		}
	}
}

func (l *layout) set(r, c int, k cellKind) {
	if r < 0 || r >= l.side || c < 0 || c >= l.side {
		return
	}
	if l.grid[r][c] != kindData {
		return // don't overwrite structural cells
	}
	l.grid[r][c] = k
}

// dataCoords returns (row,col) pairs for all data cells, in the snake order
// used by QR codes (right-to-left columns, alternating up/down).
func (l *layout) dataCoords() [][2]int {
	var coords [][2]int
	s := l.side
	up := true
	for col := s - 1; col >= 1; col -= 2 {
		if col == timingOffset-1 {
			col-- // skip timing column
		}
		var rows []int
		if up {
			for r := s - 1; r >= 0; r-- {
				rows = append(rows, r)
			}
		} else {
			for r := 0; r < s; r++ {
				rows = append(rows, r)
			}
		}
		for _, r := range rows {
			for _, c := range []int{col, col - 1} {
				if c >= 0 && l.grid[r][c] == kindData {
					coords = append(coords, [2]int{r, c})
				}
			}
		}
		up = !up
	}
	return coords
}

// finderOrigins returns the top-left corner (row,col) of each finder pattern
// inside the inner grid.
func finderOrigins(side int) [][2]int {
	last := side - finderSize
	return [][2]int{
		{0, 0},      // top-left
		{0, last},   // top-right
		{last, 0},   // bottom-left
	}
}

// minInnerSide returns the smallest inner-grid side that fits n data bits.
func minInnerSide(nBits, bitsPerCell int) int {
	for side := 21; side <= 177; side++ {
		l := newLayout(side)
		if len(l.dataCoords())*bitsPerCell >= nBits {
			return side
		}
	}
	return -1 // payload too large
}
