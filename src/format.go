package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
)

// --- File format of the scans ---
//
// Selectable per scan or watcher via ScanRequest.Format ("png" or "jpeg")
// and ScanRequest.Quality (JPEG only, 1–100). Defaults to PNG,
// without a quality SARAH_JPEG_QUALITY applies.

const (
	formatPNG  = "png"
	formatJPEG = "jpeg"
)

// normalizeFormat brings the format into normal form ("" → png, "jpg" → jpeg)
// and validates the JPEG quality.
func normalizeFormat(sr *ScanRequest) error {
	switch strings.ToLower(strings.TrimSpace(sr.Format)) {
	case "", formatPNG:
		sr.Format = formatPNG
		sr.Quality = 0
	case formatJPEG, "jpg":
		sr.Format = formatJPEG
		if sr.Quality == 0 {
			sr.Quality = settings.JPEGQuality
		}
		if sr.Quality < 1 || sr.Quality > 100 {
			return fmt.Errorf("Quality must be between 1 and 100, got %d", sr.Quality)
		}
	default:
		return fmt.Errorf("unknown Format %q (allowed: png, jpeg)", sr.Format)
	}
	return nil
}

// formatExt returns the file extension (with dot) for the format.
func formatExt(format string) string {
	if format == formatJPEG {
		return ".jpg"
	}
	return ".png"
}

// formatMime returns the content type for the format.
func formatMime(format string) string {
	if format == formatJPEG {
		return "image/jpeg"
	}
	return "image/png"
}

// encodeImage encodes the scan in the requested format and writes the
// resolution (dpiX/dpiY, 0 = unknown) into the metadata, so tools like
// ImageMagick or img2pdf pick the right page size.
func encodeImage(img image.Image, format string, quality int, dpiX, dpiY float64) ([]byte, error) {
	buf := new(bytes.Buffer)
	switch format {
	case formatJPEG:
		// The JPEG encoder only writes a single-channel JPEG for *image.Gray.
		// Convert grayscale and lineart scans first, this roughly halves the file.
		if img.ColorModel() == color.GrayModel || img.ColorModel() == color.Gray16Model {
			if _, ok := img.(*image.Gray); !ok {
				g := image.NewGray(img.Bounds())
				draw.Draw(g, g.Bounds(), img, img.Bounds().Min, draw.Src)
				img = g
			}
		}
		if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, err
		}
		return jpegWithDPI(buf.Bytes(), dpiX, dpiY), nil
	default:
		if err := png.Encode(buf, img); err != nil {
			return nil, err
		}
		return pngWithDPI(buf.Bytes(), dpiX, dpiY), nil
	}
}

// --- Resolution in the metadata ---
//
// Neither image/png nor image/jpeg write a resolution. Without one, most
// programs assume 72 dpi, which turns an A4 scan at 300 dpi into a PDF page
// more than 80 cm tall.

// pngWithDPI inserts a pHYs chunk (pixels per metre) right after the IHDR chunk.
func pngWithDPI(data []byte, dpiX, dpiY float64) []byte {
	// Signature (8) + IHDR (4 length + 4 type + 13 data + 4 CRC)
	const ihdrEnd = 8 + 25
	if dpiX <= 0 || dpiY <= 0 || len(data) < ihdrEnd || string(data[12:16]) != "IHDR" {
		return data
	}
	chunk := make([]byte, 0, 21)
	chunk = binary.BigEndian.AppendUint32(chunk, 9)
	chunk = append(chunk, "pHYs"...)
	chunk = binary.BigEndian.AppendUint32(chunk, uint32(math.Round(dpiX/0.0254)))
	chunk = binary.BigEndian.AppendUint32(chunk, uint32(math.Round(dpiY/0.0254)))
	chunk = append(chunk, 1) // unit: metre
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))

	out := make([]byte, 0, len(data)+len(chunk))
	out = append(out, data[:ihdrEnd]...)
	out = append(out, chunk...)
	return append(out, data[ihdrEnd:]...)
}

// jpegWithDPI inserts a JFIF APP0 segment with the resolution in dpi right after SOI.
// If the file already has a JFIF segment, only its resolution is set.
func jpegWithDPI(data []byte, dpiX, dpiY float64) []byte {
	if dpiX <= 0 || dpiY <= 0 || len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return data
	}
	x := uint16(min(math.Round(dpiX), math.MaxUint16))
	y := uint16(min(math.Round(dpiY), math.MaxUint16))

	// Existing JFIF: FF E0, length (2), "JFIF" + NUL, version (2), unit, X (2), Y (2)
	if len(data) >= 20 && data[2] == 0xFF && data[3] == 0xE0 && string(data[6:11]) == "JFIF\x00" {
		out := bytes.Clone(data)
		out[13] = 1 // unit: dpi
		binary.BigEndian.PutUint16(out[14:], x)
		binary.BigEndian.PutUint16(out[16:], y)
		return out
	}

	app0 := []byte{0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x01}
	app0 = binary.BigEndian.AppendUint16(app0, x)
	app0 = binary.BigEndian.AppendUint16(app0, y)
	app0 = append(app0, 0, 0) // no thumbnail

	out := make([]byte, 0, len(data)+len(app0))
	out = append(out, data[:2]...)
	out = append(out, app0...)
	return append(out, data[2:]...)
}
