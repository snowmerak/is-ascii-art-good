package ascii

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
)

// Cell represents a single character cell with its color information.
type Cell struct {
	Char rune
	R    uint8
	G    uint8
	B    uint8
}

// Art represents the complete ASCII art grid.
type Art struct {
	Width      int
	Height     int
	OrigWidth  int
	OrigHeight int
	Cells      []Cell
}

// Palette is the sequence of characters ordered by brightness/density.
// This is suitable for dark background terminals.
var Palette = []rune(" .:-=+*#%@")

// LoadImage loads an image file from the given path.
func LoadImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open image: %w", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	return img, nil
}

// ResizeBilinear resizes the input image to the target width,
// adjusting height using the character aspect ratio (default 0.5).
func ResizeBilinear(img image.Image, targetWidth int, charAspectRatio float64) image.Image {
	bounds := img.Bounds()
	origW := bounds.Dx()
	origH := bounds.Dy()

	if origW == 0 || origH == 0 {
		return image.NewRGBA(image.Rect(0, 0, 0, 0))
	}

	scaleX := float64(origW) / float64(targetWidth)
	targetHeight := int(math.Round((float64(origH) / scaleX) * charAspectRatio))
	if targetHeight < 1 {
		targetHeight = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	scaleY := float64(origH) / float64(targetHeight)

	for y := 0; y < targetHeight; y++ {
		for x := 0; x < targetWidth; x++ {
			srcX := float64(x) * scaleX
			srcY := float64(y) * scaleY

			x0 := int(math.Floor(srcX))
			y0 := int(math.Floor(srcY))
			x1 := x0 + 1
			y1 := y0 + 1

			if x1 >= origW {
				x1 = origW - 1
			}
			if y1 >= origH {
				y1 = origH - 1
			}

			dx := srcX - float64(x0)
			dy := srcY - float64(y0)

			r00, g00, b00, a00 := img.At(bounds.Min.X+x0, bounds.Min.Y+y0).RGBA()
			r10, g10, b10, a10 := img.At(bounds.Min.X+x1, bounds.Min.Y+y0).RGBA()
			r01, g01, b01, a01 := img.At(bounds.Min.X+x0, bounds.Min.Y+y1).RGBA()
			r11, g11, b11, a11 := img.At(bounds.Min.X+x1, bounds.Min.Y+y1).RGBA()

			// Interpolate colors (using 16-bit to float64, then back to 8-bit)
			r := interpolate(float64(r00), float64(r10), float64(r01), float64(r11), dx, dy) / 257.0
			g := interpolate(float64(g00), float64(g10), float64(g01), float64(g11), dx, dy) / 257.0
			b := interpolate(float64(b00), float64(b10), float64(b01), float64(b11), dx, dy) / 257.0
			a := interpolate(float64(a00), float64(a10), float64(a01), float64(a11), dx, dy) / 257.0

			// Handle alpha blending on a black background
			alpha := a / 255.0
			rf := uint8(math.Min(255, math.Max(0, r*alpha)))
			gf := uint8(math.Min(255, math.Max(0, g*alpha)))
			bf := uint8(math.Min(255, math.Max(0, b*alpha)))

			dst.SetRGBA(x, y, color.RGBA{R: rf, G: gf, B: bf, A: 255})
		}
	}

	return dst
}

func interpolate(c00, c10, c01, c11, dx, dy float64) float64 {
	return (1-dx)*(1-dy)*c00 + dx*(1-dy)*c10 + (1-dx)*dy*c01 + dx*dy*c11
}

// ConvertToASCII converts an image to ASCII art.
// It assumes the input image has already been resized to target dimensions.
func ConvertToASCII(img image.Image, origW, origH int) *Art {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	cells := make([]Cell, width*height)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := img.At(bounds.Min.X+x, bounds.Min.Y+y)
			r, g, b, _ := c.RGBA()

			// Convert to 8-bit
			r8 := uint8(r >> 8)
			g8 := uint8(g >> 8)
			b8 := uint8(b >> 8)

			// Calculate brightness (Luminance formula)
			lum := 0.299*float64(r8) + 0.587*float64(g8) + 0.114*float64(b8)

			// Map to palette character
			paletteLen := len(Palette)
			idx := int(math.Round(lum * float64(paletteLen-1) / 255.0))
			if idx < 0 {
				idx = 0
			} else if idx >= paletteLen {
				idx = paletteLen - 1
			}

			cells[y*width+x] = Cell{
				Char: Palette[idx],
				R:    r8,
				G:    g8,
				B:    b8,
				// A is assumed to be 255 from image resizing/blending
			}
		}
	}

	return &Art{
		Width:      width,
		Height:     height,
		OrigWidth:  origW,
		OrigHeight: origH,
		Cells:      cells,
	}
}
