package ascii

import (
	"image/color"
	"math"
	"sort"
)

// Quantize groups the colors in the Art into a palette of up to 256 colors
// and returns the palette (as RGBA colors) and a slice of indices representing each cell's color
// quantized using Floyd-Steinberg error diffusion dithering.
func Quantize(art *Art) ([]color.RGBA, []byte) {
	if len(art.Cells) == 0 {
		return nil, nil
	}

	width := art.Width
	height := art.Height

	// 1. Group pixels into 15-bit colors (5 bits per channel) to count frequencies.
	// This groups very similar colors together.
	counts := make(map[color.RGBA]int)
	for _, cell := range art.Cells {
		qColor := color.RGBA{
			R: cell.R & 0xF8,
			G: cell.G & 0xF8,
			B: cell.B & 0xF8,
			A: 255,
		}
		counts[qColor]++
	}

	// 2. Convert to slice and sort by frequency descending.
	type ColorCount struct {
		Color color.RGBA
		Count int
	}
	list := make([]ColorCount, 0, len(counts))
	for c, count := range counts {
		list = append(list, ColorCount{Color: c, Count: count})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Count > list[j].Count
	})

	// 3. Select the top colors up to 256.
	paletteSize := 256
	if len(list) < paletteSize {
		paletteSize = len(list)
	}
	palette := make([]color.RGBA, paletteSize)
	for i := 0; i < paletteSize; i++ {
		palette[i] = list[i].Color
	}

	// 4. Initialize error diffusion buffer (float64 for precision).
	errBuf := make([]float64, len(art.Cells)*3)
	for i, cell := range art.Cells {
		errBuf[i*3] = float64(cell.R)
		errBuf[i*3+1] = float64(cell.G)
		errBuf[i*3+2] = float64(cell.B)
	}

	indices := make([]byte, len(art.Cells))

	// 5. Run Floyd-Steinberg Dithering
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := y*width + x

			// Clamp current color (with accumulated errors) to 0-255
			cR := clamp(errBuf[idx*3])
			cG := clamp(errBuf[idx*3+1])
			cB := clamp(errBuf[idx*3+2])

			// Find nearest palette color
			bestIdx := findNearestColorFloat(cR, cG, cB, palette)
			indices[idx] = byte(bestIdx)

			// Calculate quantization error
			errR := cR - float64(palette[bestIdx].R)
			errG := cG - float64(palette[bestIdx].G)
			errB := cB - float64(palette[bestIdx].B)

			// Diffuse errors to neighbors
			// Right (x+1, y) : 7/16
			if x+1 < width {
				nIdx := idx + 1
				errBuf[nIdx*3] += errR * 7.0 / 16.0
				errBuf[nIdx*3+1] += errG * 7.0 / 16.0
				errBuf[nIdx*3+2] += errB * 7.0 / 16.0
			}
			// Bottom-Left (x-1, y+1) : 3/16
			if y+1 < height && x-1 >= 0 {
				nIdx := (y+1)*width + (x - 1)
				errBuf[nIdx*3] += errR * 3.0 / 16.0
				errBuf[nIdx*3+1] += errG * 3.0 / 16.0
				errBuf[nIdx*3+2] += errB * 3.0 / 16.0
			}
			// Bottom (x, y+1) : 5/16
			if y+1 < height {
				nIdx := (y+1)*width + x
				errBuf[nIdx*3] += errR * 5.0 / 16.0
				errBuf[nIdx*3+1] += errG * 5.0 / 16.0
				errBuf[nIdx*3+2] += errB * 5.0 / 16.0
			}
			// Bottom-Right (x+1, y+1) : 1/16
			if y+1 < height && x+1 < width {
				nIdx := (y+1)*width + (x + 1)
				errBuf[nIdx*3] += errR * 1.0 / 16.0
				errBuf[nIdx*3+1] += errG * 1.0 / 16.0
				errBuf[nIdx*3+2] += errB * 1.0 / 16.0
			}
		}
	}

	return palette, indices
}

func findNearestColorFloat(r, g, b float64, palette []color.RGBA) int {
	minDist := math.MaxFloat64
	bestIdx := 0
	for i, p := range palette {
		dR := r - float64(p.R)
		dG := g - float64(p.G)
		dB := b - float64(p.B)
		dist := dR*dR + dG*dG + dB*dB
		if dist < minDist {
			minDist = dist
			bestIdx = i
		}
	}
	return bestIdx
}

func clamp(val float64) float64 {
	if val < 0.0 {
		return 0.0
	}
	if val > 255.0 {
		return 255.0
	}
	return val
}
