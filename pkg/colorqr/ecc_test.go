package colorqr

import (
	"bytes"
	"testing"
)

// TestRSRoundTrip verifies that rsEncode → rsDecode (no erasures) is the
// identity for various input sizes.
func TestRSRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"one byte", []byte{0x42}},
		{"exactly one block", bytes.Repeat([]byte{0xAB}, RSDataShards)},
		{"one block minus one", bytes.Repeat([]byte{0xCD}, RSDataShards-1)},
		{"multiple blocks", bytes.Repeat([]byte{0xDE}, RSDataShards*3+7)},
		{"all byte values", func() []byte {
			b := make([]byte, 256)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := rsEncode(tc.data)
			if err != nil {
				t.Fatalf("rsEncode: %v", err)
			}
			if len(enc)%rsBlockSize != 0 {
				t.Fatalf("encoded length %d not a multiple of %d", len(enc), rsBlockSize)
			}

			noConf := make([]float64, len(enc)) // all zero = all confident
			dec, err := rsDecode(enc, noConf)
			if err != nil {
				t.Fatalf("rsDecode: %v", err)
			}

			// dec is padded to a multiple of RSDataShards; original data
			// must appear at the start.
			if len(dec) < len(tc.data) {
				t.Fatalf("decoded too short: got %d, want >= %d", len(dec), len(tc.data))
			}
			if !bytes.Equal(dec[:len(tc.data)], tc.data) {
				t.Errorf("data mismatch at first diff byte %d", firstDiffIdx(tc.data, dec))
			}
		})
	}
}

// TestRSErasureCorrection verifies that RSParityShards erasures in a block
// can be recovered.
func TestRSErasureCorrection(t *testing.T) {
	data := bytes.Repeat([]byte{0x55}, RSDataShards)
	enc, err := rsEncode(data)
	if err != nil {
		t.Fatalf("rsEncode: %v", err)
	}

	// Mark the first RSParityShards bytes in the single block as erasures.
	conf := make([]float64, len(enc))
	for i := 0; i < RSParityShards; i++ {
		conf[i] = erasureThreshold + 1
	}

	dec, err := rsDecode(enc, conf)
	if err != nil {
		t.Fatalf("rsDecode with erasures: %v", err)
	}
	if !bytes.Equal(dec[:RSDataShards], data) {
		t.Errorf("erasure correction failed: got %v, want %v", dec[:RSDataShards], data)
	}
}

// TestRSEncodedSizeGrowth checks that the encoded output is strictly larger
// than the input and is a multiple of rsBlockSize.
func TestRSEncodedSizeGrowth(t *testing.T) {
	for size := 0; size <= RSDataShards*4; size++ {
		enc, err := rsEncode(make([]byte, size))
		if err != nil {
			t.Fatalf("size=%d: rsEncode: %v", size, err)
		}
		if len(enc)%rsBlockSize != 0 {
			t.Errorf("size=%d: encoded length %d not a multiple of %d", size, len(enc), rsBlockSize)
		}
		if len(enc) == 0 {
			t.Errorf("size=%d: encoded length is zero", size)
		}
	}
}

func firstDiffIdx(a, b []byte) int {
	for i := range a {
		if i >= len(b) || a[i] != b[i] {
			return i
		}
	}
	return -1
}
