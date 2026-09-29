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
// library) once the palette patterns are added back.
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
			if got := len(l.data) + 8*int(s); got != want {
				t.Errorf("v%d %s: %d data+palette modules, QR has %d", v, s, got, want)
			}
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
	if got := (formatInfo{FourColor, ECMedium, 5}).bits(); got != 0b100000011001110 {
		t.Errorf("format bits = %015b", got)
	}
	// Known QR value: version 7 → 000111110010010100.
	if got := versionBits(7); got != 0b000111110010010100 {
		t.Errorf("version bits = %018b", got)
	}
	// Every format word survives any two bit errors.
	for _, s := range []Scheme{FourColor, EightColor} {
		for l := ECLow; l <= ECHigh; l++ {
			for m := range 8 {
				f := formatInfo{s, l, m}
				w := f.bits()
				for i := range 15 {
					for j := i; j < 15; j++ {
						got, d := decodeFormat(w ^ 1<<i ^ 1<<j)
						if got != f || d != bits.OnesCount32(1<<i^1<<j) {
							t.Fatalf("%+v with bits %d,%d flipped decoded as %+v", f, i, j, got)
						}
					}
				}
			}
		}
	}
}

func TestInterleaveRoundTrip(t *testing.T) {
	for _, total := range []int{26, 254, 255, 256, 700, 3706} {
		b, err := newBlockLayout(total, ECMedium)
		if err != nil {
			t.Fatal(err)
		}
		data := make([]byte, b.dataCapacity())
		for i := range data {
			data[i] = byte(i * 7)
		}
		cw := b.interleave(data)
		if len(cw) != total {
			t.Fatalf("total %d: interleaved %d codewords", total, len(cw))
		}
		got, n, err := b.deinterleave(cw)
		if err != nil || n != 0 || !bytes.Equal(got, data) {
			t.Fatalf("total %d: round trip failed (corrected %d, err %v)", total, n, err)
		}
	}
}
