// colorqr is a command-line tool for encoding and decoding HCC2D-inspired
// 2D color barcodes.
//
// Usage:
//
//	colorqr encode -o out.png "Hello, world!"
//	colorqr encode -o out.png -cell 20 < payload.bin
//	colorqr decode out.png
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"git.foxden.network/FoxDen/colorqr/pkg/colorqr"
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
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func encodeCmd(args []string) error {
	fs := flag.NewFlagSet("encode", flag.ExitOnError)
	outFile := fs.String("o", "out.png", "output PNG file path")
	cellPx := fs.Int("cell", colorqr.DefaultCellPx, "pixels per cell")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var data []byte
	if fs.NArg() > 0 {
		// Data supplied as command-line argument.
		data = []byte(fs.Arg(0))
	} else {
		// Read from stdin.
		var err error
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
	}

	f, err := os.Create(*outFile)
	if err != nil {
		return fmt.Errorf("create %s: %w", *outFile, err)
	}
	defer f.Close()

	opts := &colorqr.EncodeOptions{CellPx: *cellPx}
	if err := colorqr.Encode(f, data, opts); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	fmt.Printf("Encoded %d bytes → %s\n", len(data), *outFile)
	return nil
}

func decodeCmd(args []string) error {
	fs := flag.NewFlagSet("decode", flag.ExitOnError)
	outFile := fs.String("o", "", "write decoded bytes to file (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("decode: expected input file argument")
	}

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("open %s: %w", fs.Arg(0), err)
	}
	defer f.Close()

	data, err := colorqr.Decode(f, nil)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	var w io.Writer
	if *outFile == "" {
		w = os.Stdout
	} else {
		out, err := os.Create(*outFile)
		if err != nil {
			return fmt.Errorf("create %s: %w", *outFile, err)
		}
		defer out.Close()
		w = out
	}

	_, err = w.Write(data)
	return err
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `colorqr – 2D color barcode encoder/decoder

Usage:
  colorqr encode [flags] [<text>]
  colorqr decode [flags] <file.png>

Encode flags:
  -o <file>   Output PNG file (default: out.png)
  -cell <n>   Pixels per cell (default: 10)

  If <text> is omitted, payload is read from stdin.

Decode flags:
  -o <file>   Write decoded bytes to file (default: stdout)

Examples:
  colorqr encode -o hello.png "Hello, world!"
  colorqr decode hello.png
  echo "binary data" | colorqr encode -o data.png
  colorqr decode -o recovered.bin data.png`)
}
