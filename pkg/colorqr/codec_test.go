package colorqr_test

import (
	"bytes"
	"strings"
	"testing"

	"git.foxden.network/FoxDen/colorqr/pkg/colorqr"
)

// roundTrip encodes data and immediately decodes it, returning the recovered
// bytes.  It is the primary correctness invariant: decode(encode(x)) == x.
func roundTrip(t *testing.T, data []byte, opts *colorqr.EncodeOptions) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := colorqr.Encode(&buf, data, opts); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := colorqr.Decode(&buf, nil)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return got
}

func TestRoundTrip_ShortText(t *testing.T) {
	input := []byte("Hello, color QR!")
	got := roundTrip(t, input, nil)
	if !bytes.Equal(got, input) {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestRoundTrip_Empty(t *testing.T) {
	got := roundTrip(t, []byte{}, nil)
	if len(got) != 0 {
		t.Errorf("expected empty payload, got %d bytes: %q", len(got), got)
	}
}

func TestRoundTrip_Binary(t *testing.T) {
	// All 256 byte values.
	input := make([]byte, 256)
	for i := range input {
		input[i] = byte(i)
	}
	got := roundTrip(t, input, nil)
	if !bytes.Equal(got, input) {
		t.Errorf("binary round-trip failed at first diff byte %d", firstDiff(input, got))
	}
}

func TestRoundTrip_LongText(t *testing.T) {
	input := []byte(strings.Repeat("colorqr ", 100)) // 800 bytes
	got := roundTrip(t, input, nil)
	if !bytes.Equal(got, input) {
		t.Errorf("long text round-trip failed")
	}
}

func TestRoundTrip_LargeCellPx(t *testing.T) {
	input := []byte("Large cells")
	opts := &colorqr.EncodeOptions{CellPx: 20}
	got := roundTrip(t, input, opts)
	if !bytes.Equal(got, input) {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestRoundTrip_CustomPalette(t *testing.T) {
	// Verify that the encoder accepts a custom palette and the decoder
	// (with the matching palette hint) recovers the data correctly.
	input := []byte("custom palette test")
	opts := &colorqr.EncodeOptions{
		Palette: colorqr.FourColorPalette,
	}
	var buf bytes.Buffer
	if err := colorqr.Encode(&buf, input, opts); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decOpts := &colorqr.DecodeOptions{Palette: colorqr.FourColorPalette}
	got, err := colorqr.Decode(&buf, decOpts)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestEncode_InvalidPaletteSize(t *testing.T) {
	opts := &colorqr.EncodeOptions{
		Scheme:  colorqr.FourColor,
		Palette: colorqr.FourColorPalette[:2], // wrong length
	}
	err := colorqr.Encode(bytes.NewBuffer(nil), []byte("x"), opts)
	if err == nil {
		t.Fatal("expected error for mismatched palette size, got nil")
	}
}

func TestDecode_CorruptImage(t *testing.T) {
	_, err := colorqr.Decode(strings.NewReader("not a png"), nil)
	if err == nil {
		t.Fatal("expected error decoding garbage input, got nil")
	}
}

// TestRoundTrip_NullBytes ensures null bytes in the payload survive intact
// (regression: length-header parsing must not treat 0x00 as a terminator).
func TestRoundTrip_NullBytes(t *testing.T) {
	input := []byte{0x00, 0x01, 0x00, 0xFF, 0x00}
	got := roundTrip(t, input, nil)
	if !bytes.Equal(got, input) {
		t.Errorf("null-byte round-trip failed: got %v, want %v", got, input)
	}
}

// firstDiff returns the index of the first differing byte, or -1.
func firstDiff(a, b []byte) int {
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	for i := 0; i < limit; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return -1
}
