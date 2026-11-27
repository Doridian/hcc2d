package colorqr

import (
	"bytes"
	"math/bits"
	"math/rand/v2"
	"testing"
)

func TestRSCorrectsUpToHalfParity(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, tc := range []struct{ k, n int }{{10, 4}, {19, 7}, {100, 40}, {200, 55}} {
		for trial := range 50 {
			data := make([]byte, tc.k)
			for i := range data {
				data[i] = byte(rng.IntN(256))
			}
			block := append(append([]byte{}, data...), rsEncode(data, tc.n)...)
			nerr := rng.IntN(tc.n/2 + 1)
			for _, pos := range rng.Perm(len(block))[:nerr] {
				block[pos] ^= byte(1 + rng.IntN(255))
			}
			got, err := rsDecode(block, tc.n)
			if err != nil {
				t.Fatalf("k=%d n=%d trial %d: %d errors: %v", tc.k, tc.n, trial, nerr, err)
			}
			if got != nerr || !bytes.Equal(block[:tc.k], data) {
				t.Fatalf("k=%d n=%d trial %d: corrected %d of %d errors, data ok=%v",
					tc.k, tc.n, trial, got, nerr, bytes.Equal(block[:tc.k], data))
			}
		}
	}
}

func TestRSRejectsTooManyErrors(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	data := make([]byte, 50)
	rejected := 0
	for range 100 {
		for i := range data {
			data[i] = byte(rng.IntN(256))
		}
		block := append(append([]byte{}, data...), rsEncode(data, 10)...)
		for _, pos := range rng.Perm(len(block))[:8] {
			block[pos] ^= byte(1 + rng.IntN(255))
		}
		if _, err := rsDecode(block, 10); err != nil {
			rejected++
		}
	}
	// Beyond the correction radius a decoder may miscorrect, but it must
	// detect the overwhelming majority of cases.
	if rejected < 90 {
		t.Fatalf("only %d/100 uncorrectable blocks detected", rejected)
	}
}

// TestLayoutMatchesQR checks the QR structure against the closed-form data
// module count of ISO/IEC 18004 (as popularized by Project Nayuki's QR
// library): HCC2D's palette patterns lie outside the QR grid, so the data
// modules are exactly QR's.
func TestLayoutMatchesQR(t *testing.T) {
	for v := MinVersion; v <= MaxVersion; v++ {
		want := (16*v+128)*v + 64
		if v >= 2 {
			na := v/7 + 2
			want -= (25*na-10)*na - 55
			if v >= 7 {
				want -= 36
			}
		}
		for _, s := range []Scheme{FourColor, EightColor} {
			l, err := newLayout(v, s)
			if s == EightColor && v == 1 {
				if err == nil {
					t.Errorf("v1: 8-color palette patterns should not fit")
				}
				continue
			}
			if err != nil {
				t.Fatalf("v%d %s: %v", v, s, err)
			}
			if len(l.data) != want {
				t.Errorf("v%d %s: %d data modules, QR has %d", v, s, len(l.data), want)
			}
		}
	}
}

// TestCodeTables checks that the HCC2D code tables are QR's multiplied by
// the bits per module, remainder bits included (Grillo et al., IMCSIT 2010).
func TestCodeTables(t *testing.T) {
	for v := MinVersion; v <= MaxVersion; v++ {
		l, _ := newLayout(v, FourColor)
		qrTotal := len(l.data) / 8
		remainder := len(l.data) % 8
		for level := ECLow; level <= ECHigh; level++ {
			sum := 0
			for _, g := range qrBlocks[v][level] {
				sum += g.count * g.total
				if g.total-g.data != qrBlocks[v][level][0].total-qrBlocks[v][level][0].data {
					t.Fatalf("v%d %s: unequal parity per block", v, level)
				}
			}
			if sum != qrTotal {
				t.Fatalf("v%d %s: QR table has %d codewords, symbol %d", v, level, sum, qrTotal)
			}
			for _, s := range []Scheme{FourColor, EightColor} {
				b := newBlockLayout(v, level, s)
				bpm := s.BitsPerModule()
				if b.total() != qrTotal*bpm {
					t.Errorf("v%d %s %s: %d codewords, want %d", v, level, s, b.total(), qrTotal*bpm)
				}
				if got := len(l.data)*bpm - 8*b.total(); got != remainder*bpm {
					t.Errorf("v%d %s %s: %d remainder bits, want %d", v, level, s, got, remainder*bpm)
				}
			}
		}
	}
	// Spot checks against ISO/IEC 18004 data capacities.
	for _, c := range []struct {
		v     int
		level ECLevel
		data  int
	}{{1, ECLow, 19}, {1, ECHigh, 9}, {5, ECQuartile, 62}, {40, ECLow, 2956}, {40, ECHigh, 1276}} {
		if got := newBlockLayout(c.v, c.level, FourColor).dataCapacity(); got != 2*c.data {
			t.Errorf("v%d %s: 4-color data capacity %d, want 2×%d", c.v, c.level, got, c.data)
		}
	}
}

// TestPaletteStripsMatchFigure3 checks the palette patterns against the
// version 4, 8-color symbol of Figure 3 in Querini and Italiano (FedCSIS
// 2013): two palette cycles on the top and left, three on the bottom and
// right, all in the ring just outside the QR grid.
func TestPaletteStripsMatchFigure3(t *testing.T) {
	s := paletteStrips(33, 8)
	want := [4][2]point{
		{{8, -1}, {23, -1}}, // top, left to right
		{{-1, 24}, {-1, 9}}, // left, bottom to top
		{{8, 33}, {31, 33}}, // bottom, left to right
		{{33, 8}, {33, 31}}, // right, top to bottom
	}
	for i, strip := range s {
		if strip[0] != want[i][0] || strip[len(strip)-1] != want[i][1] {
			t.Errorf("strip %d runs %v…%v, want %v…%v", i, strip[0], strip[len(strip)-1], want[i][0], want[i][1])
		}
	}
	// The paper's 4-color, 149×149 experiment symbols: palette cells in
	// whole cycles only.
	for _, strip := range paletteStrips(149, 4) {
		if len(strip)%4 != 0 {
			t.Errorf("strip of %d cells is not whole cycles", len(strip))
		}
	}
}

func TestAlignmentTable(t *testing.T) {
	for v := 2; v <= MaxVersion; v++ {
		na := v/7 + 2
		step := 26
		if v != 32 {
			step = (v*4 + na*2 + 1) / (na*2 - 2) * 2
		}
		want := make([]int, na)
		want[0] = 6
		for i, pos := na-1, sideForVersion(v)-7; i >= 1; i, pos = i-1, pos-step {
			want[i] = pos
		}
		got := alignmentPositions[v]
		if len(got) != na {
			t.Fatalf("v%d: %v, want %v", v, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("v%d: %v, want %v", v, got, want)
			}
		}
	}
}

func TestFormatInfo(t *testing.T) {
	// Known QR value: EC level M, mask 5 → 100000011001110.
	if got := (formatInfo{ECMedium, 5}).bits(); got != 0b100000011001110 {
		t.Errorf("format bits = %015b", got)
	}
	// Known QR value: version 7 → 000111110010010100.
	if got := versionBits(7); got != 0b000111110010010100 {
		t.Errorf("version bits = %018b", got)
	}
	// Every format word survives any three bit errors.
	for l := ECLow; l <= ECHigh; l++ {
		for m := range 8 {
			f := formatInfo{l, m}
			w := f.bits()
			for i := range 15 {
				for j := i; j < 15; j++ {
					for k := j; k < 15; k++ {
						e := uint32(1<<i ^ 1<<j ^ 1<<k)
						got, d := decodeFormat(w^e, w^e)
						if got != f || d != bits.OnesCount32(e) {
							t.Fatalf("%+v with error %015b decoded as %+v", f, e, got)
						}
					}
				}
			}
		}
	}
}

func TestInterleaveRoundTrip(t *testing.T) {
	for _, c := range []struct {
		v     int
		level ECLevel
		s     Scheme
	}{{1, ECLow, FourColor}, {5, ECQuartile, FourColor}, {5, ECHigh, EightColor}, {40, ECMedium, EightColor}} {
		b := newBlockLayout(c.v, c.level, c.s)
		data := make([]byte, b.dataCapacity())
		for i := range data {
			data[i] = byte(i * 7)
		}
		cw := b.interleave(data)
		if len(cw) != b.total() {
			t.Fatalf("%+v: interleaved %d codewords", c, len(cw))
		}
		got, n, err := b.deinterleave(cw)
		if err != nil || n != 0 || !bytes.Equal(got, data) {
			t.Fatalf("%+v: round trip failed (corrected %d, err %v)", c, n, err)
		}
	}
}

func TestSegments(t *testing.T) {
	for _, c := range []struct {
		data string
		mode Mode
	}{
		{"", ModeByte},
		{"0", ModeNumeric},
		{"0123456789", ModeNumeric},
		{"00", ModeNumeric},
		{"HTTPS://EXAMPLE.COM/A-B", ModeAlphanumeric},
		{"A", ModeAlphanumeric},
		{"Hello", ModeByte},
		{"\x00\xff", ModeByte},
	} {
		if m := chooseMode([]byte(c.data)); m != c.mode {
			t.Errorf("%q: mode %s, want %s", c.data, m, c.mode)
		}
		for _, s := range []Scheme{FourColor, EightColor} {
			for _, v := range []int{2, 10, 27} {
				var w bitWriter
				writeSegment(&w, c.mode, []byte(c.data), v, s)
				if w.n != segmentBits(c.mode, len(c.data), v, s) {
					t.Errorf("%q: wrote %d bits, segmentBits says %d", c.data, w.n, segmentBits(c.mode, len(c.data), v, s))
				}
				got, err := readSegments(w.bytes(), v, s)
				if err != nil || string(got) != c.data {
					t.Errorf("%q v%d %s: read %q, %v", c.data, v, s, got, err)
				}
			}
		}
	}
	// HCC2D count widths: byte mode 16 bits; others QR + ceil(BpM/2).
	for _, c := range []struct {
		m    Mode
		v    int
		s    Scheme
		want int
	}{{ModeByte, 1, FourColor, 16}, {ModeNumeric, 1, FourColor, 11}, {ModeNumeric, 1, EightColor, 12}, {ModeAlphanumeric, 27, FourColor, 14}} {
		if got := countBits(c.m, c.v, c.s); got != c.want {
			t.Errorf("countBits(%s, v%d, %s) = %d, want %d", c.m, c.v, c.s, got, c.want)
		}
	}
}
