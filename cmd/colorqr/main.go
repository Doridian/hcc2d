// colorqr is a command-line tool for encoding and decoding HCC2D 2D color
// barcodes.
//
// Usage:
//
//	colorqr encode -o out.png -colors 8 "Hello, world!"
//	colorqr encode -o out.png -ec H < payload.bin
//	colorqr decode out.png
package main

import (
	"flag"
	"fmt"
	"image/png"
	"io"
	"os"

	"git.foxden.network/FoxDen/hcc2d/pkg/colorqr"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}
	switch args[0] {
	case "encode":
		return encodeCmd(args[1:])
	case "decode":
		return decodeCmd(args[1:])
	case "-h", "-help", "--help", "help":
		printUsage()
		return nil
	}
	printUsage()
	return fmt.Errorf("unknown command: %s", args[0])
}

func encodeCmd(args []string) error {
	fs := flag.NewFlagSet("encode", flag.ExitOnError)
	outFile := fs.String("o", "out.png", "output PNG file")
	colors := fs.Int("colors", 4, "number of colors: 4 (2 bits/module) or 8 (3 bits/module)")
	ec := fs.String("ec", "L", "error correction level: L, M, Q or H")
	version := fs.Int("version", 1, "minimum symbol version (1-40)")
	modulePx := fs.Int("module", colorqr.DefaultModulePx, "pixels per module")
	quiet := fs.Int("quiet", colorqr.DefaultQuietZone, "quiet zone in modules")
	if err := fs.Parse(args); err != nil {
		return err
	}
	level, err := colorqr.ParseECLevel(*ec)
	if err != nil {
		return err
	}

	var data []byte
	if fs.NArg() > 0 {
		data = []byte(fs.Arg(0))
	} else if data, err = io.ReadAll(os.Stdin); err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	opts := &colorqr.EncodeOptions{
		Scheme:     colorqr.Scheme(*colors),
		Level:      level,
		MinVersion: *version,
	}
	sym, err := colorqr.NewSymbol(data, opts)
	if err != nil {
		return err
	}
	f, err := os.Create(*outFile)
	if err != nil {
		return err
	}
	if err := png.Encode(f, sym.Image(max(1, *modulePx), max(0, *quiet))); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "encoded %d bytes → %s (version %d, %d×%d modules, %s, EC %s, mask %d)\n",
		len(data), *outFile, sym.Version, sym.Size(), sym.Size(), sym.Scheme, sym.Level, sym.Mask)
	return nil
}

func decodeCmd(args []string) error {
	fs := flag.NewFlagSet("decode", flag.ExitOnError)
	outFile := fs.String("o", "", "write decoded bytes to file (default: stdout)")
	classifier := fs.String("classifier", "kmeans", "color classifier: kmeans or euclidean")
	verbose := fs.Bool("v", false, "print symbol information to stderr")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("decode: expected one input image")
	}
	opts := &colorqr.DecodeOptions{}
	switch *classifier {
	case "kmeans":
		opts.Classifier = colorqr.KMeans
	case "euclidean":
		opts.Classifier = colorqr.MinDistance
	default:
		return fmt.Errorf("unknown classifier %q", *classifier)
	}

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		return err
	}
	res, err := colorqr.Decode(f, opts)
	_ = f.Close()
	if err != nil {
		return err
	}
	if *verbose {
		fmt.Fprintf(os.Stderr, "version %d, %s, EC %s, mask %d, %d bytes corrected\n",
			res.Version, res.Scheme, res.Level, res.Mask, res.Corrected)
	}

	if *outFile == "" {
		_, err = os.Stdout.Write(res.Data)
		return err
	}
	return os.WriteFile(*outFile, res.Data, 0o644)
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `colorqr – HCC2D 2D color barcode encoder/decoder

Usage:
  colorqr encode [flags] [<text>]
  colorqr decode [flags] <image>

Encode flags:
  -o <file>        output PNG file (default out.png)
  -colors <4|8>    4 colors = 2 bits/module, 8 colors = 3 bits/module (default 4)
  -ec <L|M|Q|H>    error correction level (default L)
  -version <n>     minimum symbol version 1-40 (default 1)
  -module <px>     pixels per module (default 8)
  -quiet <n>       quiet zone in modules (default 4)

  If <text> is omitted, the payload is read from stdin.

Decode flags:
  -o <file>                       write decoded bytes to file (default stdout)
  -classifier <kmeans|euclidean>  color classifier (default kmeans)
  -v                              print symbol information to stderr

Examples:
  colorqr encode -o hello.png "Hello, world!"
  colorqr encode -colors 8 -ec M -o data.png < file.bin
  colorqr decode -v hello.png`)
}
