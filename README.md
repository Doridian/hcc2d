# colorqr

A Go implementation of an HCC2D-inspired 2D **color** barcode, based on:

> Querini & Italiano, "Color Classifiers for 2D Color Barcodes", FedCSIS 2013.

A copy can be found in `paper.pdf`

## Key design choices

| Aspect | Choice |
|--------|--------|
| Color scheme | 4-color (2 bits/cell) |
| Palette | Black, Cyan, Magenta, White |
| Color space for classification | YUV (BT.601) – separates luma and chroma as recommended by the paper |
| Classifier | Minimum Euclidean distance in YUV (fast, no training needed) |
| Palette strips | 2-cell-wide strips on the bottom and right edges for adaptive palette recovery (k-means) |
| Structure | QR-style finder patterns (7×7) + timing patterns + format strip |
| Quiet zone | 1 cell |
| Error correction | None (intentionally omitted; add Reed-Solomon on top) |

The decoder in this repo handles only **perfectly aligned images** (no rotation
or perspective correction).  The paper identifies this as a distinct phase from
color classification, and implementing geometric correction would require a
separate homography step.

## Build

```sh
go build -o colorqr .
```

## CLI usage

```sh
# Encode a text string
./colorqr encode -o hello.png "Hello, color QR!"

# Encode binary stdin
cat file.bin | ./colorqr encode -o file.png

# Decode (prints to stdout)
./colorqr decode hello.png

# Decode to a file
./colorqr decode -o recovered.bin file.png

# Larger cells (easier to print/scan)
./colorqr encode -cell 20 -o big.png "Hello!"
```

## Library usage

```go
import "colorqr/colorqr"

// Encode
var buf bytes.Buffer
err := colorqr.Encode(&buf, []byte("Hello!"), nil)

// Decode
data, err := colorqr.Decode(&buf, nil)
```

## Run tests

```sh
go test ./colorqr/...
```

## Limitations & future work

- 8-color scheme (3 bits/cell) is defined but not yet wired up in the encoder.
- The decoder assumes a perfectly aligned scan; adding perspective correction
  (using the finder pattern centroids to compute a homography) would enable
  real-world use.
- The paper found k-means outperforms the minimum-distance classifier; the
  k-means path is used when reading palette strips but not yet as the primary
  cell classifier.
- Reed-Solomon error correction should be added between the payload and the
  bit stream for reliable use in noisy channels.
