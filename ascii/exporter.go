package ascii

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

// Font8x8 is a custom 8x8 bitmap font for the palette characters: " .:-=+*#%@"
var Font8x8 = map[rune][8]byte{
	' ': {0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	'.': {0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x18, 0x18},
	':': {0x00, 0x18, 0x18, 0x00, 0x00, 0x18, 0x18, 0x00},
	'-': {0x00, 0x00, 0x00, 0x7e, 0x00, 0x00, 0x00, 0x00},
	'=': {0x00, 0x00, 0x7e, 0x00, 0x7e, 0x00, 0x00, 0x00},
	'+': {0x00, 0x18, 0x18, 0x7e, 0x18, 0x18, 0x00, 0x00},
	'*': {0x00, 0x24, 0x18, 0x7e, 0x18, 0x24, 0x00, 0x00},
	'#': {0x00, 0x24, 0x7e, 0x24, 0x7e, 0x24, 0x00, 0x00},
	'%': {0x00, 0x62, 0x64, 0x08, 0x13, 0x23, 0x00, 0x00},
	'@': {0x00, 0x3c, 0x42, 0x5a, 0x5a, 0x3c, 0x00, 0x00},
}

// ExportPixel reconstructs a W x H image where each pixel matches the cell's color.
func ExportPixel(art *Art, outputPath string) error {
	img := image.NewRGBA(image.Rect(0, 0, art.Width, art.Height))

	for y := 0; y < art.Height; y++ {
		for x := 0; x < art.Width; x++ {
			cell := art.Cells[y*art.Width+x]
			img.SetRGBA(x, y, color.RGBA{R: cell.R, G: cell.G, B: cell.B, A: 255})
		}
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output image: %w", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		return fmt.Errorf("failed to encode PNG: %w", err)
	}

	return nil
}

// ExportRender reconstructs a high-resolution image by drawing ASCII glyphs.
func ExportRender(art *Art, outputPath string) error {
	dstW := art.Width * 8
	dstH := art.Height * 8
	img := image.NewRGBA(image.Rect(0, 0, dstW, dstH))

	// Draw characters cell by cell
	for y := 0; y < art.Height; y++ {
		for x := 0; x < art.Width; x++ {
			cell := art.Cells[y*art.Width+x]
			glyph, ok := Font8x8[cell.Char]
			if !ok {
				glyph = Font8x8[' ']
			}

			// Draw 8x8 glyph
			for gy := 0; gy < 8; gy++ {
				rowByte := glyph[gy]
				for gx := 0; gx < 8; gx++ {
					pixelX := x*8 + gx
					pixelY := y*8 + gy

					// Check if bit (7-gx) is set (MSB is leftmost pixel)
					if (rowByte & (0x80 >> gx)) != 0 {
						img.SetRGBA(pixelX, pixelY, color.RGBA{R: cell.R, G: cell.G, B: cell.B, A: 255})
					} else {
						img.SetRGBA(pixelX, pixelY, color.RGBA{R: 0, G: 0, B: 0, A: 255}) // Black background
					}
				}
			}
		}
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output image: %w", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		return fmt.Errorf("failed to encode PNG: %w", err)
	}

	return nil
}
