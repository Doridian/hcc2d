package colorqr

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"sort"
)

const (
	// DefaultModulePx is the default rendered size of a module in pixels.
	DefaultModulePx = 8
	// DefaultQuietZone is the default margin in modules, as in QR.
	DefaultQuietZone = 4
)

// maxPayload is the largest byte-mode payload (16-bit character count).
const maxPayload = 1<<16 - 1

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

	ModulePx int // pixels per module for Encode (default DefaultModulePx)
	// QuietZone is the margin in modules for Encode, counted from the edge
	// of the QR grid (default DefaultQuietZone). The innermost ring holds
	// the palette patterns, so at least 2 is used.
	QuietZone int
}

// Symbol is an encoded HCC2D symbol.
type Symbol struct {
	Version int
	Scheme  Scheme
	Level   ECLevel
	Mask    int
	Mode    Mode // data segment mode

	layout  *layout
	palette Palette
	dark    [][]bool      // function modules
	value   [][]int       // palette index of data modules, -1 elsewhere
	ring    map[point]int // palette pattern cells outside the QR grid
}

// Size returns the number of modules per side of the QR grid. The palette
// patterns lie one module outside it, at coordinates -1 and Size().
func (s *Symbol) Size() int { return s.layout.side }

// Palette returns the symbol's colors; module value i has color i.
func (s *Symbol) Palette() Palette { return s.palette }

// At returns the color of module (x, y) for -1 <= x, y <= Size(). Cells of
// the outer ring that are not part of a palette pattern are white.
func (s *Symbol) At(x, y int) color.RGBA {
	n := s.Size()
	if x < 0 || y < 0 || x >= n || y >= n {
		if v, ok := s.ring[point{x, y}]; ok {
			return s.palette[v]
		}
		return colorWhite
	}
	if s.value[y][x] < 0 {
		if s.dark[y][x] {
			return colorBlack
		}
		return colorWhite
	}
	return s.palette[s.value[y][x]]
}

// Image renders the symbol with modulePx pixels per module and a quietZone
// module margin around the QR grid (at least 2, since the innermost ring
// holds the palette patterns).
func (s *Symbol) Image(modulePx, quietZone int) *image.RGBA {
	modulePx = max(1, modulePx)
	quietZone = max(2, quietZone)
	n := (s.Size() + 2*quietZone) * modulePx
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for y := -1; y <= s.Size(); y++ {
		for x := -1; x <= s.Size(); x++ {
			c := s.At(x, y)
			if c == colorWhite {
				continue
			}
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

// NewSymbol encodes data into the smallest symbol that fits, starting at
// opts.MinVersion.
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
	if o.MinVersion < MinVersion || o.MinVersion > MaxVersion {
		return nil, fmt.Errorf("minimum version %d out of range", o.MinVersion)
	}
	if len(data) > maxPayload {
		return nil, fmt.Errorf("payload of %d bytes exceeds the maximum of %d", len(data), maxPayload)
	}

	mode := chooseMode(data)
	for v := o.MinVersion; v <= MaxVersion; v++ {
		l, err := newLayout(v, o.Scheme)
		if err != nil {
			continue // palette patterns do not fit yet
		}
		blocks := newBlockLayout(v, o.Level, o.Scheme)
		if len(data) > maxCount(mode, v, o.Scheme) || blocks.dataCapacity()*8 < segmentBits(mode, len(data), v, o.Scheme) {
			continue
		}
		return buildSymbol(l, blocks, o, mode, data), nil
	}
	return nil, fmt.Errorf("payload of %d bytes does not fit in a version %d %s symbol at EC level %s",
		len(data), MaxVersion, o.Scheme, o.Level)
}

// Capacity returns the maximum byte-mode payload in bytes of a symbol with
// the given version, scheme and error correction level, or 0 if no such
// symbol exists.
func Capacity(version int, s Scheme, level ECLevel) int {
	return CapacityMode(version, s, level, ModeByte)
}

// CapacityMode is like Capacity for a payload of numeric or alphanumeric
// characters (see Mode).
func CapacityMode(version int, s Scheme, level ECLevel, m Mode) int {
	if !s.valid() || !level.valid() {
		return 0
	}
	if _, err := newLayout(version, s); err != nil {
		return 0
	}
	bits := newBlockLayout(version, level, s).dataCapacity() * 8
	n := 0
	for step := maxPayload + 1; step > 0; step /= 2 {
		if k := n + step; k <= maxCount(m, version, s) && segmentBits(m, k, version, s) <= bits {
			n = k
		}
	}
	return n
}

func buildSymbol(l *layout, blocks blockLayout, o EncodeOptions, mode Mode, data []byte) *Symbol {
	// One data segment, terminator and QR pad bytes.
	var bw bitWriter
	writeSegment(&bw, mode, data, l.version, o.Scheme)
	capBits := blocks.dataCapacity() * 8
	bw.write(0, min(4, capBits-bw.n))
	bw.write(0, (8-bw.n%8)%8)
	for pad := byte(0xec); bw.n < capBits; pad ^= 0xec ^ 0x11 {
		bw.write(uint32(pad), 8)
	}

	// Error correction, then split the codeword stream into module values.
	// The remainder bits after the last codeword stay zero.
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
		Mode:    mode,
		layout:  l,
		palette: o.Palette,
		dark:    newBoolGrid(l.side),
		value:   make([][]int, l.side),
		ring:    map[point]int{},
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
			sym.ring[p] = i
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

	fb := formatInfo{s.Level, m}.bits()
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

// penalty scores mask m with the four QR rules. As in Grillo et al.
// (IMCSIT 2010), the symbol is seen as a binary matrix of dark modules (the
// darker half of the palette) and light ones, because that is what finder
// detection works on.
func (s *Symbol) penalty() int {
	n := s.Size()
	dark := newBoolGrid(n)
	darkValue := s.darkValues()
	darkCount := 0
	for y := range n {
		for x := range n {
			if v := s.value[y][x]; v >= 0 {
				dark[y][x] = darkValue[v]
			} else {
				dark[y][x] = s.dark[y][x]
			}
			if dark[y][x] {
				darkCount++
			}
		}
	}
	at := func(x, y int, transpose bool) bool {
		if transpose {
			return dark[x][y]
		}
		return dark[y][x]
	}

	score := 0
	for _, t := range []bool{false, true} {
		for y := range n {
			// Rule 1: runs of five or more modules of the same color.
			run := 1
			for x := 1; x <= n; x++ {
				if x < n && at(x, y, t) == at(x-1, y, t) {
					run++
					continue
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
					if at(x+k, y, t) {
						w |= 1 << (10 - k)
					}
				}
				if w == 0b10111010000 || w == 0b00001011101 {
					score += 40
				}
			}
		}
	}
	// Rule 2: 2×2 blocks of the same color.
	for y := 0; y+1 < n; y++ {
		for x := 0; x+1 < n; x++ {
			c := dark[y][x]
			if dark[y][x+1] == c && dark[y+1][x] == c && dark[y+1][x+1] == c {
				score += 3
			}
		}
	}
	// Rule 4: balance of dark and light modules.
	pct := darkCount * 100 / (n * n)
	score += abs(pct-50) / 5 * 10
	return score
}

// darkValues reports which palette entries are in the darker half by luma.
func (s *Symbol) darkValues() []bool {
	idx := make([]int, len(s.palette))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return rgbaToYUV(s.palette[idx[a]]).Y < rgbaToYUV(s.palette[idx[b]]).Y
	})
	dark := make([]bool, len(s.palette))
	for _, i := range idx[:len(idx)/2] {
		dark[i] = true
	}
	return dark
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
