//go:build js && wasm

// colorqr-wasm exposes the HCC2D encoder and decoder to JavaScript as the
// global object hcc2d, for the demo web app in web/.
//
//	hcc2d.encode(data, {colors, ec, minVersion, palette}) → symbol
//	hcc2d.decode(rgba, width, height, {classifier})     → result
//	hcc2d.capacity(version, colors, ec)                  → bytes
//
// data and rgba are Uint8Arrays; palette is a list of "#rrggbb" strings.
// Every call returns {ok: false, error} on failure.
package main

import (
	"fmt"
	"image"
	"image/color"
	"strconv"
	"strings"
	"syscall/js"
	"time"
	"unicode/utf8"

	"git.foxden.network/FoxDen/hcc2d/pkg/colorqr"
)

func main() {
	js.Global().Set("hcc2d", js.ValueOf(map[string]any{
		"encode":   js.FuncOf(guard(encode)),
		"decode":   js.FuncOf(guard(decode)),
		"capacity": js.FuncOf(guard(capacity)),
	}))
	if ready := js.Global().Get("hcc2dReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {}
}

// guard turns errors and panics into {ok: false, error}.
func guard(f func(args []js.Value) (map[string]any, error)) func(js.Value, []js.Value) any {
	return func(_ js.Value, args []js.Value) (out any) {
		defer func() {
			if r := recover(); r != nil {
				out = map[string]any{"ok": false, "error": fmt.Sprint(r)}
			}
		}()
		res, err := f(args)
		if err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		res["ok"] = true
		return res
	}
}

func opt(args []js.Value, i int, key string) js.Value {
	if len(args) <= i || args[i].Type() != js.TypeObject {
		return js.Undefined()
	}
	return args[i].Get(key)
}

func intOr(v js.Value, def int) int {
	if v.Type() == js.TypeNumber {
		return v.Int()
	}
	return def
}

func level(v js.Value) (colorqr.ECLevel, error) {
	if v.Type() != js.TypeString {
		return colorqr.ECLow, nil
	}
	return colorqr.ParseECLevel(v.String())
}

func bytesOf(v js.Value) []byte {
	if v.Type() == js.TypeString {
		return []byte(v.String())
	}
	b := make([]byte, v.Get("length").Int())
	js.CopyBytesToGo(b, v)
	return b
}

func uint8Array(b []byte) js.Value {
	a := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(a, b)
	return a
}

func hexColor(c color.RGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

func parseHex(s string) (color.RGBA, error) {
	s = strings.TrimPrefix(s, "#")
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil || len(s) != 6 {
		return color.RGBA{}, fmt.Errorf("invalid color %q", s)
	}
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, nil
}

// encode returns the module grid including the palette ring: cells holds
// (size+2)² RGB triples, row by row from module (-1, -1).
func encode(args []js.Value) (map[string]any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("encode: missing data")
	}
	data := bytesOf(args[0])
	lv, err := level(opt(args, 1, "ec"))
	if err != nil {
		return nil, err
	}
	o := &colorqr.EncodeOptions{
		Scheme:     colorqr.Scheme(intOr(opt(args, 1, "colors"), 4)),
		Level:      lv,
		MinVersion: max(1, intOr(opt(args, 1, "minVersion"), 1)),
	}
	if p := opt(args, 1, "palette"); p.Type() == js.TypeObject && p.Length() > 0 {
		for i := range p.Length() {
			c, err := parseHex(p.Index(i).String())
			if err != nil {
				return nil, err
			}
			o.Palette = append(o.Palette, c)
		}
	}
	sym, err := colorqr.NewSymbol(data, o)
	if err != nil {
		return nil, err
	}
	n := sym.Size()
	cells := make([]byte, 0, 3*(n+2)*(n+2))
	for y := -1; y <= n; y++ {
		for x := -1; x <= n; x++ {
			c := sym.At(x, y)
			cells = append(cells, c.R, c.G, c.B)
		}
	}
	pal := make([]any, len(sym.Palette()))
	for i, c := range sym.Palette() {
		pal[i] = hexColor(c)
	}
	capBytes := colorqr.CapacityMode(sym.Version, sym.Scheme, sym.Level, sym.Mode)
	return map[string]any{
		"version":  sym.Version,
		"size":     n,
		"colors":   int(sym.Scheme),
		"ec":       sym.Level.String(),
		"mask":     sym.Mask,
		"mode":     sym.Mode.String(),
		"bytes":    len(data),
		"capacity": capBytes,
		"palette":  pal,
		"cells":    uint8Array(cells),
	}, nil
}

func decode(args []js.Value) (map[string]any, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("decode: want rgba, width, height")
	}
	w, h := args[1].Int(), args[2].Int()
	img := &image.NRGBA{Pix: bytesOf(args[0]), Stride: 4 * w, Rect: image.Rect(0, 0, w, h)}
	if len(img.Pix) < 4*w*h {
		return nil, fmt.Errorf("decode: %d bytes of pixel data for a %d×%d image", len(img.Pix), w, h)
	}
	o := &colorqr.DecodeOptions{}
	if c := opt(args, 3, "classifier"); c.Type() == js.TypeString && c.String() == "euclidean" {
		o.Classifier = colorqr.MinDistance
	}
	start := time.Now()
	res, err := colorqr.DecodeImage(img, o)
	if err != nil {
		return nil, err
	}
	corners := make([]any, 4)
	for i, c := range res.Corners {
		corners[i] = []any{c.X, c.Y}
	}
	out := map[string]any{
		"data":      uint8Array(res.Data),
		"version":   res.Version,
		"size":      17 + 4*res.Version,
		"colors":    int(res.Scheme),
		"ec":        res.Level.String(),
		"mask":      res.Mask,
		"corrected": res.Corrected,
		"corners":   corners,
		"ms":        float64(time.Since(start).Microseconds()) / 1000,
	}
	if utf8.Valid(res.Data) {
		out["text"] = string(res.Data)
	}
	return out, nil
}

func capacity(args []js.Value) (map[string]any, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("capacity: want version, colors, ec")
	}
	lv, err := colorqr.ParseECLevel(args[2].String())
	if err != nil {
		return nil, err
	}
	return map[string]any{"bytes": colorqr.Capacity(args[0].Int(), colorqr.Scheme(args[1].Int()), lv)}, nil
}
