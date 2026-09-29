package colorqr

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
)

const (
	// DefaultModulePx is the default rendered size of a module in pixels.
	DefaultModulePx = 8
	// DefaultQuietZone is the default margin in modules, as in QR.
	DefaultQuietZone = 4

	modeByte  = 0b0100 // QR byte-mode indicator
	countBits = 16     // character count width (colors raise capacity past QR's 8-bit V1-9 limit)
)

// EncodeOptions controls symbol construction. The zero value produces a
// 4-color symbol at EC level L in the smallest version that fits.
type EncodeOptions struct {
	Scheme     Scheme  // FourColor (default) or EightColor
	Level      ECLevel // error correction level (default ECLow)
	MinVersion int     // smallest version to consider (default 1)
	// Palette overrides the default colors; it must have Scheme entries.
	// The decoder learns colors from the palette patterns, so no matching
	// decoder setting is needed.
	Palette Palette

	ModulePx  int // pixels per module for Encode (default DefaultModulePx)
	QuietZone int // margin in modules for Encode (default DefaultQuietZone)
}

// Symbol is an encoded HCC2D symbol.
type Symbol struct {
	Version int
	Scheme  Scheme
	Level   ECLevel
	Mask    int

	layout  *layout
	palette Palette
	dark    [][]bool // function modules
	value   [][]int  // palette index of data and palette modules
}

// Size returns the number of modules per side.
func (s *Symbol) Size() int { return s.layout.side }

// At returns the color of module (x, y).
func (s *Symbol) At(x, y int) color.RGBA {
	if s.value[y][x] < 0 {
		if s.dark[y][x] {
			return colorBlack
		}
		return colorWhite
	}
	return s.palette[s.value[y][x]]
}

// Image renders the symbol with modulePx pixels per module and a quietZone
// module margin.
func (s *Symbol) Image(modulePx, quietZone int) *image.RGBA {
	n := (s.Size() + 2*quietZone) * modulePx
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for y := range s.Size() {
		for x := range s.Size() {
			c := s.At(x, y)
			x0, y0 := (x+quietZone)*modulePx, (y+quietZone)*modulePx
			for py := y0; py < y0+modulePx; py++ {
				for px := x0; px < x0+modulePx; px++ {
					img.SetRGBA(px, py, c)
				}
			}
		}
	}
	return img
}

// Encode writes data as an HCC2D symbol in PNG format to w.
func Encode(w io.Writer, data []byte, opts *EncodeOptions) error {
	sym, err := NewSymbol(data, opts)
	if err != nil {
		return err
	}
	var o EncodeOptions
	if opts != nil {
		o = *opts
	}
	if o.ModulePx <= 0 {
		o.ModulePx = DefaultModulePx
	}
	if o.QuietZone <= 0 {
		o.QuietZone = DefaultQuietZone
	}
	return png.Encode(w, sym.Image(o.ModulePx, o.QuietZone))
}

// NewSymbol encodes data into the smallest symbol that fits.
func NewSymbol(data []byte, opts *EncodeOptions) (*Symbol, error) {
	var o EncodeOptions
	if opts != nil {
		o = *opts
	}
	if o.Scheme == 0 {
		o.Scheme = FourColor
	}
	if !o.Scheme.valid() {
		return nil, fmt.Errorf("unsupported color scheme %d", int(o.Scheme))
	}
	if !o.Level.valid() {
		return nil, fmt.Errorf("invalid error correction level %d", int(o.Level))
	}
	if o.Palette == nil {
		o.Palette = o.Scheme.DefaultPalette()
	}
	if len(o.Palette) != int(o.Scheme) {
		return nil, fmt.Errorf("palette has %d colors, %s scheme needs %d", len(o.Palette), o.Scheme, int(o.Scheme))
	}
	if o.MinVersion == 0 {
		o.MinVersion = MinVersion
	}
	if len(data) >= 1<<countBits {
		return nil, fmt.Errorf("payload of %d bytes exceeds the maximum of %d", len(data), 1<<countBits-1)
	}

	needBits := 4 + countBits + 8*len(data)
	for v := o.MinVersion; v <= MaxVersion; v++ {
		l, err := newLayout(v, o.Scheme)
		if err != nil {
			continue // palette patterns do not fit yet
		}
		blocks, err := newBlockLayout(l.codewords(), o.Level)
		if err != nil || blocks.dataCapacity()*8 < needBits {
			continue
		}
		return buildSymbol(l, blocks, o, data), nil
	}
	return nil, fmt.Errorf("payload of %d bytes does not fit in a version %d %s symbol at EC level %s",
		len(data), MaxVersion, o.Scheme, o.Level)
}

// Capacity returns the maximum payload in bytes of a symbol with the given
// version, scheme and error correction level, or 0 if no such symbol exists.
func Capacity(version int, s Scheme, level ECLevel) int {
	if !s.valid() || !level.valid() {
		return 0
	}
	l, err := newLayout(version, s)
	if err != nil {
		return 0
	}
	b, err := newBlockLayout(l.codewords(), level)
	if err != nil {
		return 0
	}
	return min(max(0, (b.dataCapacity()*8-4-countBits)/8), 1<<countBits-1)
}

func buildSymbol(l *layout, blocks blockLayout, o EncodeOptions, data []byte) *Symbol {
	// Byte-mode segment, terminator and QR pad bytes.
	var bw bitWriter
	bw.write(modeByte, 4)
	bw.write(uint32(len(data)), countBits)
	for _, b := range data {
		bw.write(uint32(b), 8)
	}
	capBits := blocks.dataCapacity() * 8
	bw.write(0, min(4, capBits-bw.n))
	bw.write(0, (8-bw.n%8)%8)
	for pad := byte(0xec); bw.n < capBits; pad ^= 0xec ^ 0x11 {
		bw.write(uint32(pad), 8)
	}

	// Error correction, then split the codeword stream into module values.
	cw := blocks.interleave(bw.bytes())
	bpm := o.Scheme.BitsPerModule()
	br := bitReader{buf: cw}
	values := make([]int, len(l.data))
	for i := range values {
		values[i] = int(br.read(bpm))
	}

	sym := &Symbol{
		Version: l.version,
		Scheme:  o.Scheme,
		Level:   o.Level,
		layout:  l,
		palette: o.Palette,
		dark:    newBoolGrid(l.side),
		value:   make([][]int, l.side),
	}
	for y := range sym.value {
		sym.value[y] = make([]int, l.side)
		for x := range sym.value[y] {
			sym.value[y][x] = -1
			sym.dark[y][x] = l.fixed[y][x]
		}
	}
	for i, pts := range l.palette {
		for _, p := range pts {
			sym.value[p.y][p.x] = i
		}
	}

	best, bestPenalty := 0, -1
	for m := range 8 {
		sym.applyMask(m, values)
		if p := sym.penalty(); bestPenalty < 0 || p < bestPenalty {
			best, bestPenalty = m, p
		}
	}
	sym.applyMask(best, values)
	return sym
}

// applyMask places values in the data modules under mask m and writes the
// matching format and version information.
func (s *Symbol) applyMask(m int, values []int) {
	l := s.layout
	full := int(s.Scheme) - 1
	for i, p := range l.data {
		v := values[i]
		if maskBit(m, p.x, p.y) {
			v ^= full
		}
		s.value[p.y][p.x] = v
	}
	s.Mask = m

	fb := formatInfo{s.Scheme, s.Level, m}.bits()
	for _, copyPos := range formatPositions(l.side) {
		for i, p := range copyPos {
			s.dark[p.y][p.x] = fb>>i&1 == 1
		}
	}
	if l.version >= 7 {
		vb := versionBits(l.version)
		for _, copyPos := range versionPositions(l.side) {
			for i, p := range copyPos {
				s.dark[p.y][p.x] = vb>>i&1 == 1
			}
		}
	}
}

// penalty adapts the QR mask evaluation to color: rules 1 and 2 (runs and
// 2×2 blocks) look at identical colors, while rules 3 and 4 (finder-like
// patterns and dark/light balance) look at the symbol as a scanner's
// black-and-white binarization sees it, since that is what finder
// detection works on.
func (s *Symbol) penalty() int {
	n := s.Size()
	id := make([][]int, n)
	dark := newBoolGrid(n)
	darkCount := 0
	for y := range n {
		id[y] = make([]int, n)
		for x := range n {
			c := s.At(x, y)
			id[y][x] = int(c.R)<<16 | int(c.G)<<8 | int(c.B)
			dark[y][x] = rgbaToYUV(c).Y < 128
			if dark[y][x] {
				darkCount++
			}
		}
	}
	at := func(x, y int, transpose bool) (int, bool) {
		if transpose {
			x, y = y, x
		}
		return id[y][x], dark[y][x]
	}

	score := 0
	for _, t := range []bool{false, true} {
		for y := range n {
			run := 1
			for x := 1; x <= n; x++ {
				if x < n {
					a, _ := at(x, y, t)
					b, _ := at(x-1, y, t)
					if a == b {
						run++
						continue
					}
				}
				if run >= 5 {
					score += 3 + run - 5
				}
				run = 1
			}
			// Rule 3: 1:1:3:1:1 with four light modules on either side.
			for x := 0; x+11 <= n; x++ {
				var w uint
				for k := range 11 {
					if _, d := at(x+k, y, t); d {
						w |= 1 << (10 - k)
					}
				}
				if w == 0b10111010000 || w == 0b00001011101 {
					score += 40
				}
			}
		}
	}
	for y := 0; y+1 < n; y++ {
		for x := 0; x+1 < n; x++ {
			c := id[y][x]
			if id[y][x+1] == c && id[y+1][x] == c && id[y+1][x+1] == c {
				score += 3
			}
		}
	}
	pct := darkCount * 100 / (n * n)
	score += abs(pct-50) / 5 * 10
	return score
}

type bitWriter struct {
	buf []byte
	n   int
}

func (w *bitWriter) write(v uint32, bits int) {
	for i := bits - 1; i >= 0; i-- {
		if w.n%8 == 0 {
			w.buf = append(w.buf, 0)
		}
		if v>>i&1 == 1 {
			w.buf[w.n/8] |= 0x80 >> (w.n % 8)
		}
		w.n++
	}
}

func (w *bitWriter) bytes() []byte { return w.buf }

// bitReader reads MSB-first; reads past the end yield zero bits (the
// remainder bits of the symbol).
type bitReader struct {
	buf []byte
	n   int
}

func (r *bitReader) read(bits int) uint32 {
	var v uint32
	for range bits {
		v <<= 1
		if r.n/8 < len(r.buf) && r.buf[r.n/8]&(0x80>>(r.n%8)) != 0 {
			v |= 1
		}
		r.n++
	}
	return v
}
