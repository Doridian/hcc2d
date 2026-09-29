package colorqr

import (
	"fmt"
	"image/color"
)

// Scheme is the number of colors used in the data region.
type Scheme int

const (
	FourColor  Scheme = 4 // 2 bits per module
	EightColor Scheme = 8 // 3 bits per module
)

// BitsPerModule returns how many data bits each colored module carries.
func (s Scheme) BitsPerModule() int {
	switch s {
	case FourColor:
		return 2
	case EightColor:
		return 3
	}
	return 0
}

func (s Scheme) valid() bool { return s == FourColor || s == EightColor }

func (s Scheme) String() string {
	if !s.valid() {
		return fmt.Sprintf("Scheme(%d)", int(s))
	}
	return fmt.Sprintf("%d-color", int(s))
}

// Palette maps a module value (its index) to a color.
type Palette []color.RGBA

// The default palettes treat each bit as one subtractive ink (C, M, Y), so
// "all bits set" is black and "no bits set" is white. The 4-color palette
// is the paper's example assignment: white=00, magenta=01, cyan=10,
// black=11. The 8-color palette uses all eight corners of the RGB cube.
var (
	FourColorPalette = Palette{
		{255, 255, 255, 255}, // 00 white
		{255, 0, 255, 255},   // 01 magenta
		{0, 255, 255, 255},   // 10 cyan
		{0, 0, 0, 255},       // 11 black
	}
	EightColorPalette = Palette{
		{255, 255, 255, 255}, // 000 white
		{255, 255, 0, 255},   // 001 yellow
		{255, 0, 255, 255},   // 010 magenta
		{255, 0, 0, 255},     // 011 red
		{0, 255, 255, 255},   // 100 cyan
		{0, 255, 0, 255},     // 101 green
		{0, 0, 255, 255},     // 110 blue
		{0, 0, 0, 255},       // 111 black
	}
)

// DefaultPalette returns the built-in palette for s.
func (s Scheme) DefaultPalette() Palette {
	if s == EightColor {
		return EightColorPalette
	}
	return FourColorPalette
}

var (
	colorBlack = color.RGBA{0, 0, 0, 255}
	colorWhite = color.RGBA{255, 255, 255, 255}
)

// yuv is a BT.601 YUV triple. The paper classifies in YUV because it
// separates luma from chroma.
type yuv struct{ Y, U, V float64 }

func toYUV(r, g, b float64) yuv {
	return yuv{
		Y: 0.299*r + 0.587*g + 0.114*b,
		U: -0.14713*r - 0.28886*g + 0.436*b,
		V: 0.615*r - 0.51499*g - 0.10001*b,
	}
}

func rgbaToYUV(c color.RGBA) yuv { return toYUV(float64(c.R), float64(c.G), float64(c.B)) }

func (a yuv) dist2(b yuv) float64 {
	dy, du, dv := a.Y-b.Y, a.U-b.U, a.V-b.V
	return dy*dy + du*du + dv*dv
}
