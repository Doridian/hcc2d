package colorqr

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"io"
	"math"
)

// DecodeOptions controls decoding parameters.
type DecodeOptions struct {
	// Palette overrides the reference palette used for nearest-color
	// classification.  When nil the decoder reads the palette from the
	// embedded palette strips (k-means, simple version).
	Palette Palette
}

// Decode reads a colorqr PNG from r and returns the original payload bytes.
//
// The decoder assumes the image is perfectly aligned (no rotation, no
// perspective distortion).  It locates the symbol by detecting the three
// finder patterns and derives the cell grid from their positions.
func Decode(r io.Reader, opts *DecodeOptions) ([]byte, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	// ── 1. locate finder patterns ─────────────────────────────────────────
	cellPx, innerOriginX, innerOriginY, side, err := locateGrid(img)
	if err != nil {
		return nil, fmt.Errorf("locate grid: %w", err)
	}

	// ── 2. sample the palette from strips ────────────────────────────────
	var palette Palette
	if opts != nil && opts.Palette != nil {
		palette = opts.Palette
	} else {
		palette = samplePaletteFromStrips(img, innerOriginX, innerOriginY, side, cellPx)
	}

	bpc := FourColor.bitsPerCell()
	if len(palette) == int(EightColor) {
		bpc = EightColor.bitsPerCell()
	}

	// ── 3. read format strip ──────────────────────────────────────────────
	// (We derive bpc from palette length above; format strip could carry
	// additional metadata in a full implementation.)

	// ── 4. build layout & extract data bits + per-cell confidence ────────
	l := newLayout(side)
	coords := l.dataCoords()

	var bits []bool
	// cellDist[i] = squared YUV distance for cell i (lower = more confident).
	cellDist := make([]float64, 0, len(coords))
	for _, pos := range coords {
		cx := innerOriginX + (pos[1])*cellPx + cellPx/2
		cy := innerOriginY + (pos[0])*cellPx + cellPx/2
		sampled := sampleCell(img, cx, cy, cellPx)
		idx, dist := nearestColorDist(sampled, palette)
		cellDist = append(cellDist, dist)
		for b := bpc - 1; b >= 0; b-- {
			bits = append(bits, (idx>>b)&1 == 1)
		}
	}

	// ── 5. bits → bytes, compute per-byte confidence ──────────────────────
	rawBytes := bitsToBytes(bits)
	cellsPerByte := 8 / bpc
	byteConf := make([]float64, len(rawBytes))
	for i := range rawBytes {
		// Confidence for byte i = max cell distance across its contributing cells.
		for k := 0; k < cellsPerByte; k++ {
			ci := i*cellsPerByte + k
			if ci < len(cellDist) && cellDist[ci] > byteConf[i] {
				byteConf[i] = cellDist[ci]
			}
		}
	}

	// ── 6. RS decode ──────────────────────────────────────────────────────
	byteData, err := rsDecode(rawBytes, byteConf)
	if err != nil {
		return nil, fmt.Errorf("rs decode: %w", err)
	}

	// ── 7. strip length header ────────────────────────────────────────────
	if len(byteData) < 4 {
		return nil, fmt.Errorf("data region too short to contain length header")
	}
	payloadLen := binary.BigEndian.Uint32(byteData[:4])
	if int(payloadLen) > len(byteData)-4 {
		return nil, fmt.Errorf("declared length %d exceeds available data %d",
			payloadLen, len(byteData)-4)
	}
	return byteData[4 : 4+payloadLen], nil
}

// ── grid location ────────────────────────────────────────────────────────────

// locateGrid scans the image for the three finder patterns and derives
// cellPx, the pixel offset of the inner grid origin, and the grid side.
//
// Strategy (perfect-alignment assumption):
//  1. Find the top-left corner of the top-left finder pattern by scanning
//     for a long run of dark pixels.
//  2. Measure cellPx from the run length (7 cells wide).
//  3. Derive innerOrigin and side from the image dimensions.
func locateGrid(img image.Image) (cellPx, originX, originY, side int, err error) {
	b := img.Bounds()
	imgW, imgH := b.Max.X-b.Min.X, b.Max.Y-b.Min.Y

	// Find the first dark pixel row by row.
	var firstDarkX, firstDarkY int
	found := false
	for y := b.Min.Y; y < b.Max.Y && !found; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if isDark(img.At(x, y)) {
				firstDarkX, firstDarkY = x, y
				found = true
				break
			}
		}
	}
	if !found {
		return 0, 0, 0, 0, fmt.Errorf("no dark pixels found in image")
	}

	// Measure the horizontal run of dark pixels starting at firstDark.
	// This run spans exactly 7 cells (the finder outer ring).
	runLen := 0
	for x := firstDarkX; x < b.Max.X && isDark(img.At(x, firstDarkY)); x++ {
		runLen++
	}
	if runLen < finderSize {
		return 0, 0, 0, 0, fmt.Errorf("finder run too short: %d px", runLen)
	}
	cellPx = runLen / finderSize

	// The inner grid starts at firstDarkX, firstDarkY (quiet zone already
	// accounted for by the dark pixel search).
	originX = firstDarkX
	originY = firstDarkY

	// Inner grid side: the image covers (side + 2*quiet)*cellPx pixels.
	// We know originX = quietZone*cellPx, so:
	// side = (imgW - 2*quietZone*cellPx) / cellPx
	side = (imgW - 2*originX) / cellPx
	_ = imgH // symmetric

	if side < 21 {
		return 0, 0, 0, 0, fmt.Errorf("computed side %d is too small", side)
	}
	return cellPx, originX, originY, side, nil
}

// ── palette sampling ─────────────────────────────────────────────────────────

// samplePaletteFromStrips reads colors from the bottom palette strip and
// clusters them to recover the reference palette via simple k-means.
//
// The strip repeats palette[i % nColors] for i = 0..side-1.  We average
// samples of the same class to get robust centroid estimates.
func samplePaletteFromStrips(img image.Image, ox, oy, side, cellPx int) Palette {
	// Collect one sample per cell from the bottom-most palette row.
	nColors := int(FourColor) // default; refined below
	samples := make([]color.RGBA, side)
	for c := 0; c < side; c++ {
		cx := ox + c*cellPx + cellPx/2
		cy := oy + (side-1)*cellPx + cellPx/2
		samples[c] = sampleCell(img, cx, cy, cellPx)
	}

	// k-means with k=nColors, initialized by the first nColors samples.
	centroids := make([]yuv, nColors)
	for i := 0; i < nColors; i++ {
		centroids[i] = rgbaToYUV(samples[i%len(samples)])
	}

	for iter := 0; iter < 20; iter++ {
		sums := make([]yuv, nColors)
		counts := make([]int, nColors)
		for _, s := range samples {
			sv := rgbaToYUV(s)
			best, _ := nearestYUV(sv, centroids)
			sums[best].Y += sv.Y
			sums[best].U += sv.U
			sums[best].V += sv.V
			counts[best]++
		}
		moved := false
		for k := range centroids {
			if counts[k] == 0 {
				continue
			}
			n := yuv{
				Y: sums[k].Y / float64(counts[k]),
				U: sums[k].U / float64(counts[k]),
				V: sums[k].V / float64(counts[k]),
			}
			if n != centroids[k] {
				moved = true
				centroids[k] = n
			}
		}
		if !moved {
			break
		}
	}

	palette := make(Palette, nColors)
	for i, c := range centroids {
		palette[i] = yuvToRGBA(c)
	}
	return palette
}

// ── color classification ─────────────────────────────────────────────────────

// nearestColor returns the index in palette that is closest to c in YUV space
// (Euclidean distance), matching the minimum-distance classifier from the paper.
func nearestColor(c color.RGBA, palette Palette) int {
	idx, _ := nearestColorDist(c, palette)
	return idx
}

// nearestColorDist returns the palette index and squared YUV distance.
func nearestColorDist(c color.RGBA, palette Palette) (int, float64) {
	cv := rgbaToYUV(c)
	refs := make([]yuv, len(palette))
	for i, p := range palette {
		refs[i] = rgbaToYUV(p)
	}
	return nearestYUV(cv, refs)
}

func nearestYUV(c yuv, refs []yuv) (int, float64) {
	best, bestDist := 0, math.MaxFloat64
	for i, r := range refs {
		d := yuvDist(c, r)
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	return best, bestDist
}

func yuvDist(a, b yuv) float64 {
	dy := a.Y - b.Y
	du := a.U - b.U
	dv := a.V - b.V
	return dy*dy + du*du + dv*dv
}

// ── color space conversion ────────────────────────────────────────────────────

// yuv holds floating-point YUV components.
type yuv struct{ Y, U, V float64 }

// rgbaToYUV converts an RGBA color to YUV (BT.601).
func rgbaToYUV(c color.RGBA) yuv {
	r := float64(c.R)
	g := float64(c.G)
	b := float64(c.B)
	return yuv{
		Y:  0.299*r + 0.587*g + 0.114*b,
		U: -0.14713*r - 0.28886*g + 0.436*b,
		V:  0.615*r - 0.51499*g - 0.10001*b,
	}
}

// yuvToRGBA converts a YUV color back to RGBA (BT.601), clamping to [0,255].
func yuvToRGBA(c yuv) color.RGBA {
	r := c.Y + 1.13983*c.V
	g := c.Y - 0.39465*c.U - 0.58060*c.V
	b := c.Y + 2.03211*c.U
	return color.RGBA{
		R: clampU8(r),
		G: clampU8(g),
		B: clampU8(b),
		A: 255,
	}
}

func clampU8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// ── pixel helpers ─────────────────────────────────────────────────────────────

// sampleCell returns the average RGBA of a cellPx×cellPx region centered at
// (cx, cy).  Averaging over the full cell reduces noise from anti-aliasing or
// scanner artifacts.
func sampleCell(img image.Image, cx, cy, cellPx int) color.RGBA {
	half := cellPx / 2
	if half == 0 {
		half = 1
	}
	var rSum, gSum, bSum, n int
	for dy := -half / 2; dy <= half/2; dy++ {
		for dx := -half / 2; dx <= half/2; dx++ {
			c := img.At(cx+dx, cy+dy)
			r, g, b, _ := c.RGBA()
			rSum += int(r >> 8)
			gSum += int(g >> 8)
			bSum += int(b >> 8)
			n++
		}
	}
	if n == 0 {
		n = 1
	}
	return color.RGBA{
		R: uint8(rSum / n),
		G: uint8(gSum / n),
		B: uint8(bSum / n),
		A: 255,
	}
}

// isDark returns true if the pixel's luma is below 128.
func isDark(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	y := (299*int(r>>8) + 587*int(g>>8) + 114*int(b>>8)) / 1000
	return y < 128
}

// ── bit/byte helpers ──────────────────────────────────────────────────────────

func bitsToBytes(bits []bool) []byte {
	nBytes := len(bits) / 8
	bs := make([]byte, nBytes)
	for i := 0; i < nBytes; i++ {
		for j := 0; j < 8; j++ {
			if bits[i*8+j] {
				bs[i] |= 1 << (7 - j)
			}
		}
	}
	return bs
}
