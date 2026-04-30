package colorqr

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
)

// EncodeOptions controls encoding parameters.
type EncodeOptions struct {
	// CellPx is the number of pixels per cell (default 10).
	CellPx int
	// Scheme selects the color palette size (default FourColor).
	Scheme ColorScheme
	// Palette overrides the default color palette.  Must match Scheme.
	Palette Palette
}

func (o *EncodeOptions) fillDefaults() {
	if o.CellPx == 0 {
		o.CellPx = defaultCellPx
	}
	if o.Scheme == 0 {
		o.Scheme = FourColor
	}
	if o.Palette == nil {
		o.Palette = FourColorPalette
	}
}

// Encode writes a colorqr PNG image encoding data to w.
func Encode(w io.Writer, data []byte, opts *EncodeOptions) error {
	if opts == nil {
		opts = &EncodeOptions{}
	}
	opts.fillDefaults()

	if len(opts.Palette) != int(opts.Scheme) {
		return fmt.Errorf("palette length %d does not match scheme %d",
			len(opts.Palette), opts.Scheme)
	}

	bpc := opts.Scheme.bitsPerCell()

	// Prepend a 4-byte big-endian length header so the decoder knows where
	// payload ends inside the padded data region.
	payload := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(payload[:4], uint32(len(data)))
	copy(payload[4:], data)

	// Convert payload bytes → bit stream.
	bits := bytesToBits(payload)

	side := minInnerSide(len(bits), bpc)
	if side < 0 {
		return fmt.Errorf("data too large to encode")
	}

	l := newLayout(side)
	coords := l.dataCoords()

	// Build inner grid of colors.
	inner := make([][]color.RGBA, side)
	for r := range inner {
		inner[r] = make([]color.RGBA, side)
	}

	// ── structural cells ──────────────────────────────────────────────────

	black := color.RGBA{A: 255}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	// Finder patterns.
	for _, origin := range finderOrigins(side) {
		for dr := 0; dr < finderSize; dr++ {
			for dc := 0; dc < finderSize; dc++ {
				if finderCell(dr, dc) {
					inner[origin[0]+dr][origin[1]+dc] = black
				} else {
					inner[origin[0]+dr][origin[1]+dc] = white
				}
			}
		}
		// Separator (white).
		for dr := -1; dr <= finderSize; dr++ {
			setInner(inner, side, origin[0]+dr, origin[1]-1, white)
			setInner(inner, side, origin[0]+dr, origin[1]+finderSize, white)
		}
		for dc := -1; dc <= finderSize; dc++ {
			setInner(inner, side, origin[0]-1, origin[1]+dc, white)
			setInner(inner, side, origin[0]+finderSize, origin[1]+dc, white)
		}
	}

	// Timing patterns.
	timingRow := finderSize - 1
	timingCol := finderSize - 1
	for i := timingOffset; i < side-timingOffset; i++ {
		if timingCell(i) {
			inner[timingRow][i] = black
			inner[i][timingCol] = black
		} else {
			inner[timingRow][i] = white
			inner[i][timingCol] = white
		}
	}

	// Format info strip (row 0 and col 0): encode scheme as 1 byte.
	// We store the scheme value (4 or 8) in the first format cell pair.
	formatByte := byte(opts.Scheme)
	formatBits := bytesToBits([]byte{formatByte, byte(opts.CellPx)})
	fi := 0
	for c := 1; c < side && fi < len(formatBits); c++ {
		if l.grid[0][c] == kindFormat {
			inner[0][c] = boolToColor(formatBits[fi], black, white)
			fi++
		}
	}
	for r := 1; r < side && fi < len(formatBits); r++ {
		if l.grid[r][0] == kindFormat {
			inner[r][0] = boolToColor(formatBits[fi], black, white)
			fi++
		}
	}

	// Palette strips (bottom 2 rows and right 2 cols): repeat palette colors.
	for i := 0; i < side; i++ {
		colorIdx := i % len(opts.Palette)
		c := opts.Palette[colorIdx]
		for d := 0; d < paletteStrip; d++ {
			setInner(inner, side, side-1-d, i, c)
			setInner(inner, side, i, side-1-d, c)
		}
	}

	// ── data cells ────────────────────────────────────────────────────────
	bitIdx := 0
	for _, pos := range coords {
		var val int
		for b := 0; b < bpc && bitIdx < len(bits); b++ {
			val = (val << 1) | btoi(bits[bitIdx])
			bitIdx++
		}
		inner[pos[0]][pos[1]] = opts.Palette[val]
	}

	// ── render to image ───────────────────────────────────────────────────
	totalSide := (side + 2*quietZone) * opts.CellPx
	img := image.NewRGBA(image.Rect(0, 0, totalSide, totalSide))

	// Quiet zone background: white.
	for py := 0; py < totalSide; py++ {
		for px := 0; px < totalSide; px++ {
			img.SetRGBA(px, py, white)
		}
	}

	for r := 0; r < side; r++ {
		for c := 0; c < side; c++ {
			cl := inner[r][c]
			startX := (c + quietZone) * opts.CellPx
			startY := (r + quietZone) * opts.CellPx
			for dy := 0; dy < opts.CellPx; dy++ {
				for dx := 0; dx < opts.CellPx; dx++ {
					img.SetRGBA(startX+dx, startY+dy, cl)
				}
			}
		}
	}

	return png.Encode(w, img)
}

// ── helpers ───────────────────────────────────────────────────────────────

func setInner(inner [][]color.RGBA, side, r, c int, cl color.RGBA) {
	if r >= 0 && r < side && c >= 0 && c < side {
		inner[r][c] = cl
	}
}

func boolToColor(b bool, dark, light color.RGBA) color.RGBA {
	if b {
		return dark
	}
	return light
}

func bytesToBits(bs []byte) []bool {
	bits := make([]bool, len(bs)*8)
	for i, b := range bs {
		for j := 0; j < 8; j++ {
			bits[i*8+j] = (b>>(7-j))&1 == 1
		}
	}
	return bits
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
