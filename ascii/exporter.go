package ascii

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
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

// ExportPixel reconstructs an image where the height is stretched to match the original aspect ratio.
func ExportPixel(art *Art, outputPath string) error {
	targetHeight := art.Height
	if art.OrigWidth > 0 && art.OrigHeight > 0 {
		targetHeight = int(math.Round(float64(art.Width) * float64(art.OrigHeight) / float64(art.OrigWidth)))
		if targetHeight < 1 {
			targetHeight = 1
		}
	}

	img := image.NewRGBA(image.Rect(0, 0, art.Width, targetHeight))

	for y := 0; y < targetHeight; y++ {
		srcY := float64(y) * float64(art.Height) / float64(targetHeight)
		y0 := int(math.Floor(srcY))
		y1 := y0 + 1
		if y1 >= art.Height {
			y1 = art.Height - 1
		}
		dy := srcY - float64(y0)

		for x := 0; x < art.Width; x++ {
			c0 := art.Cells[y0*art.Width+x]
			c1 := art.Cells[y1*art.Width+x]

			// Interpolate color in the Y direction
			r := uint8(math.Round(float64(c0.R)*(1-dy) + float64(c1.R)*dy))
			g := uint8(math.Round(float64(c0.G)*(1-dy) + float64(c1.G)*dy))
			b := uint8(math.Round(float64(c0.B)*(1-dy) + float64(c1.B)*dy))

			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
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

// ExportRender reconstructs a high-resolution image by drawing ASCII glyphs,
// adjusting cell height dynamically to restore the original aspect ratio.
func ExportRender(art *Art, outputPath string) error {
	dstW := art.Width * 8
	dstH := art.Height * 8
	if art.OrigWidth > 0 && art.OrigHeight > 0 {
		dstH = int(math.Round(float64(art.Width*8) * float64(art.OrigHeight) / float64(art.OrigWidth)))
		if dstH < 8 {
			dstH = 8
		}
	}

	img := image.NewRGBA(image.Rect(0, 0, dstW, dstH))

	// Scale each cell height dynamically
	cellHeightVal := float64(dstH) / float64(art.Height)

	for y := 0; y < art.Height; y++ {
		yStart := int(math.Round(float64(y) * cellHeightVal))
		yEnd := int(math.Round(float64(y+1) * cellHeightVal))
		if yEnd > dstH {
			yEnd = dstH
		}
		ch := yEnd - yStart

		for x := 0; x < art.Width; x++ {
			cell := art.Cells[y*art.Width+x]
			glyph, ok := Font8x8[cell.Char]
			if !ok {
				glyph = Font8x8[' ']
			}

			// Fill cell background with black
			for cy := yStart; cy < yEnd; cy++ {
				for cx := 0; cx < 8; cx++ {
					img.SetRGBA(x*8+cx, cy, color.RGBA{R: 0, G: 0, B: 0, A: 255})
				}
			}

			// Draw glyph centered vertically inside the cell
			yOffset := (ch - 8) / 2
			for gy := 0; gy < 8; gy++ {
				rowByte := glyph[gy]
				for gx := 0; gx < 8; gx++ {
					targetY := yStart + yOffset + gy
					if targetY >= yStart && targetY < yEnd {
						if (rowByte & (0x80 >> gx)) != 0 {
							img.SetRGBA(x*8+gx, targetY, color.RGBA{R: cell.R, G: cell.G, B: cell.B, A: 255})
						}
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
