// Package colorqr encodes and decodes HCC2D (High Capacity Colored
// 2-Dimensional) codes: QR-based 2D color barcodes whose data modules carry
// 2 bits (4 colors) or 3 bits (8 colors) each, following Querini and
// Italiano, "Color Classifiers for 2D Color Barcodes", FedCSIS 2013.
//
// Use [Encode] or [NewSymbol] to build a symbol and [Decode] or
// [DecodeImage] to read one back from an image.
package colorqr
