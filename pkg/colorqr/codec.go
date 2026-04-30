// Package colorqr implements a simplified HCC2D-inspired 2D color barcode
// encoder and decoder.
//
// Design overview
// ───────────────
//   - A symbol is a square grid of colored cells.
//   - Cells in the data region encode 2 bits each using a 4-color palette
//     (black, cyan, magenta, white).
//   - Four 3×3 finder patterns sit at the corners so a decoder can locate
//     and orient the symbol (identical to QR finder squares, but smaller).
//   - A 1-cell quiet zone surrounds the symbol.
//   - Four Color Palette Strips (one per corner edge) let the decoder
//     recover the distorted palette via k-means; the strips are not used in
//     this "perfect-alignment" decoder but are written so real images remain
//     decodable by a future full decoder.
//   - No error-correction is added (the paper's focus was the classifier;
//     Reed-Solomon can be layered on top).
package colorqr

import "image/color"

// ColorScheme selects how many bits each cell encodes.
type ColorScheme int

const (
	FourColor  ColorScheme = 4 // 2 bits/cell
	EightColor ColorScheme = 8 // 3 bits/cell  (future work)
)

// bitsPerCell returns the number of data bits encoded per cell.
func (cs ColorScheme) bitsPerCell() int {
	switch cs {
	case EightColor:
		return 3
	default:
		return 2
	}
}

// Palette is an ordered list of reference colors.  The index of a color is
// its numeric value; for 4-color that value is the 2-bit codeword for the
// cell.
type Palette []color.RGBA

// FourColorPalette is the default 4-color palette: black=00, cyan=01,
// magenta=10, white=11.
var FourColorPalette = Palette{
	{R: 0, G: 0, B: 0, A: 255},       // 0b00 – black
	{R: 0, G: 200, B: 200, A: 255},   // 0b01 – cyan
	{R: 200, G: 0, B: 200, A: 255},   // 0b10 – magenta
	{R: 255, G: 255, B: 255, A: 255}, // 0b11 – white
}

// Symbol holds the decoded metadata about a colorqr symbol.
type Symbol struct {
	// GridSize is the number of cells per side (including quiet zone and
	// structural cells but excluding the border quiet-zone cells when
	// accessing DataGrid).
	GridSize int
	// CellPx is the rendered size of each cell in pixels.
	CellPx int
	Scheme ColorScheme
	// Data is the raw byte payload recovered from the symbol.
	Data []byte
}

// ── layout constants ────────────────────────────────────────────────────────

const (
	quietZone      = 1  // cells of quiet zone on each side
	finderSize     = 7  // 7×7 finder pattern (QR-style)
	finderSep      = 1  // 1-cell separator around each finder
	paletteStrip   = 2  // palette strip width in cells
	timingOffset   = finderSize + finderSep // first timing cell index
	defaultCellPx = 10 // pixels per cell in generated images

	// DefaultCellPx is the exported default cell size for CLI/library callers.
	DefaultCellPx = defaultCellPx
)
