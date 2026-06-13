package ascii

import (
	"image/color"
	"math"
	"sort"
)

// Quantize groups the colors in the Art into a palette of up to 256 colors
// and returns the palette (as RGBA colors) and a slice of indices representing each cell's color.
func Quantize(art *Art) ([]color.RGBA, []byte) {
	if len(art.Cells) == 0 {
		return nil, nil
	}

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

	// 4. Map each cell's actual color to the nearest palette color index.
	indices := make([]byte, len(art.Cells))
	for i, cell := range art.Cells {
		c := color.RGBA{R: cell.R, G: cell.G, B: cell.B, A: 255}
		indices[i] = byte(FindNearestColor(c, palette))
	}

	return palette, indices
}

// FindNearestColor finds the index of the closest color in the palette using Euclidean distance.
func FindNearestColor(c color.RGBA, palette []color.RGBA) int {
	minDist := math.MaxFloat64
	bestIdx := 0
	for i, p := range palette {
		dR := float64(c.R) - float64(p.R)
		dG := float64(c.G) - float64(p.G)
		dB := float64(c.B) - float64(p.B)
		dist := dR*dR + dG*dG + dB*dB
		if dist < minDist {
			minDist = dist
			bestIdx = i
		}
	}
	return bestIdx
}
