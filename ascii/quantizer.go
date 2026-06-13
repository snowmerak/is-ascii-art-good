package ascii

import (
	"image/color"
	"math"
	"sort"
)

// QuantizeColors groups the colors in the slice into a palette of up to 256 colors
// and returns the palette (as RGBA colors) and a slice of indices representing each color's index.
func QuantizeColors(colors []color.RGBA) ([]color.RGBA, []byte) {
	if len(colors) == 0 {
		return nil, nil
	}

	// 1. Group pixels into 15-bit colors (5 bits per channel) to count frequencies.
	counts := make(map[color.RGBA]int)
	for _, c := range colors {
		qColor := color.RGBA{
			R: c.R & 0xF8,
			G: c.G & 0xF8,
			B: c.B & 0xF8,
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

	// 4. Map each color to the nearest palette color index.
	indices := make([]byte, len(colors))
	for i, c := range colors {
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
