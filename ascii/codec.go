package ascii

import (
	"encoding/binary"
	"fmt"
	"image/color"
	"io"
	"math"
	"os"

	"github.com/klauspost/compress/zstd"
)

// Magic header bytes for .gac files.
const Magic = "GASC"

// SaveGAC writes the Art using hybrid resolution (high-res 4-bit characters + configurable downscaled 8-bit colors + Zstd).
func SaveGAC(art *Art, path string, colorScale int) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	width := art.Width
	height := art.Height

	if colorScale < 1 {
		colorScale = 1
	}

	// 1. Calculate color grid dimensions
	colorWidth := width / colorScale
	if colorWidth < 1 {
		colorWidth = 1
	}
	colorHeight := height / colorScale
	if colorHeight < 1 {
		colorHeight = 1
	}

	// 2. Average colors in each block (box filter)
	colorGrid := make([]color.RGBA, colorWidth*colorHeight)
	for cy := 0; cy < colorHeight; cy++ {
		for cx := 0; cx < colorWidth; cx++ {
			xStart := cx * width / colorWidth
			xEnd := (cx + 1) * width / colorWidth
			yStart := cy * height / colorHeight
			yEnd := (cy + 1) * height / colorHeight

			var sumR, sumG, sumB int
			count := 0
			for y := yStart; y < yEnd; y++ {
				for x := xStart; x < xEnd; x++ {
					cell := art.Cells[y*width+x]
					sumR += int(cell.R)
					sumG += int(cell.G)
					sumB += int(cell.B)
					count++
				}
			}
			if count > 0 {
				colorGrid[cy*colorWidth+cx] = color.RGBA{
					R: uint8(sumR / count),
					G: uint8(sumG / count),
					B: uint8(sumB / count),
					A: 255,
				}
			}
		}
	}

	// 3. Run 256-color popularity quantization on the low-res color grid
	palette, colorIndices := QuantizeColors(colorGrid)
	paletteSize := len(palette)

	// 4. Extract character grid and pack high-res characters to 4-bit indices (density range 0-9)
	charGrid := make([]byte, width*height)
	for i := 0; i < width*height; i++ {
		charGrid[i] = getPaletteIndex(art.Cells[i].Char)
	}

	packedChars := make([]byte, (width*height+1)/2)
	for i := 0; i < width*height; i += 2 {
		val1 := charGrid[i]
		var val2 byte = 0
		if i+1 < width*height {
			val2 = charGrid[i+1]
		}
		packedChars[i/2] = (val1 << 4) | (val2 & 0x0F)
	}

	// 5. Compute edge flags for each 2x2 color block
	edgeFlagsSize := (colorWidth*colorHeight + 3) / 4
	packedEdgeFlags := make([]byte, edgeFlagsSize)

	for cy := 0; cy < colorHeight; cy++ {
		for cx := 0; cx < colorWidth; cx++ {
			xStart := cx * width / colorWidth
			xEnd := (cx + 1) * width / colorWidth
			yStart := cy * height / colorHeight
			yEnd := (cy + 1) * height / colorHeight

			var d00, d10, d01, d11 byte
			d00 = charGrid[yStart*width+xStart]

			xRight := xEnd - 1
			if xRight < xStart {
				xRight = xStart
			}
			yBottom := yEnd - 1
			if yBottom < yStart {
				yBottom = yStart
			}

			d10 = charGrid[yStart*width+xRight]
			d01 = charGrid[yBottom*width+xStart]
			d11 = charGrid[yBottom*width+xRight]

			dh := absDiff(d00, d10) + absDiff(d01, d11)
			dv := absDiff(d00, d01) + absDiff(d10, d11)

			var flag byte = 0
			if dh > 2 || dv > 2 {
				if dh > dv {
					flag = 1 // Vertical edge
				} else if dv > dh {
					flag = 2 // Horizontal edge
				} else {
					flag = 3 // Diagonal / complex edge
				}
			}

			idx := cy*colorWidth + cx
			packedEdgeFlags[idx/4] |= (flag & 0x03) << ((idx % 4) * 2)
		}
	}

	// Write magic
	if _, err := file.WriteString(Magic); err != nil {
		return fmt.Errorf("failed to write magic: %w", err)
	}

	// Write width, height, original width, original height, palette size, color width, color height (32-byte header)
	headerFields := []uint32{
		uint32(width),
		uint32(height),
		uint32(art.OrigWidth),
		uint32(art.OrigHeight),
		uint32(paletteSize),
		uint32(colorWidth),
		uint32(colorHeight),
	}
	for _, val := range headerFields {
		if err := binary.Write(file, binary.BigEndian, val); err != nil {
			return fmt.Errorf("failed to write header field: %w", err)
		}
	}

	// Create zstd writer
	zw, err := zstd.NewWriter(file)
	if err != nil {
		return fmt.Errorf("failed to create zstd writer: %w", err)
	}
	defer zw.Close()

	// Write palette data: PaletteSize * 3 bytes (R, G, B for each color)
	palBytes := make([]byte, paletteSize*3)
	for i, c := range palette {
		palBytes[i*3] = c.R
		palBytes[i*3+1] = c.G
		palBytes[i*3+2] = c.B
	}
	if _, err := zw.Write(palBytes); err != nil {
		return fmt.Errorf("failed to write palette to zstd: %w", err)
	}

	// Write packed character grid
	if _, err := zw.Write(packedChars); err != nil {
		return fmt.Errorf("failed to write packed characters to zstd: %w", err)
	}

	// Write packed edge flags
	if _, err := zw.Write(packedEdgeFlags); err != nil {
		return fmt.Errorf("failed to write packed edge flags to zstd: %w", err)
	}

	// 6. RLE compress low-res color indices
	rleColorIndices := EncodeRLE(colorIndices)

	// Write RLE color indices
	if _, err := zw.Write(rleColorIndices); err != nil {
		return fmt.Errorf("failed to write RLE color indices to zstd: %w", err)
	}

	return nil
}

// LoadGAC reads an Art from a .gac file, decompressing and upscaling color channels.
func LoadGAC(path string) (*Art, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Read magic
	magic := make([]byte, 4)
	if _, err := io.ReadFull(file, magic); err != nil {
		return nil, fmt.Errorf("failed to read magic: %w", err)
	}
	if string(magic) != Magic {
		return nil, fmt.Errorf("invalid file format magic: expected %s, got %s", Magic, string(magic))
	}

	// Read header fields (32-byte header total with magic)
	var width, height, origWidth, origHeight, paletteSize, colorWidth, colorHeight uint32
	headerFields := []*uint32{&width, &height, &origWidth, &origHeight, &paletteSize, &colorWidth, &colorHeight}
	for _, ptr := range headerFields {
		if err := binary.Read(file, binary.BigEndian, ptr); err != nil {
			return nil, fmt.Errorf("failed to read header field: %w", err)
		}
	}

	// Create zstd reader
	zr, err := zstd.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd reader: %w", err)
	}
	defer zr.Close()

	// Read palette data
	palBytes := make([]byte, paletteSize*3)
	if _, err := io.ReadFull(zr, palBytes); err != nil {
		return nil, fmt.Errorf("failed to read palette from zstd: %w", err)
	}
	palette := make([]color.RGBA, paletteSize)
	for i := 0; i < int(paletteSize); i++ {
		palette[i] = color.RGBA{
			R: palBytes[i*3],
			G: palBytes[i*3+1],
			B: palBytes[i*3+2],
			A: 255,
		}
	}

	// Read packed character grid
	packedCharSize := (int(width*height) + 1) / 2
	packedChars := make([]byte, packedCharSize)
	if _, err := io.ReadFull(zr, packedChars); err != nil {
		return nil, fmt.Errorf("failed to read packed characters from zstd: %w", err)
	}

	// Unpack packed character grid
	charIndices := make([]byte, width*height)
	for i := 0; i < packedCharSize; i++ {
		b := packedChars[i]
		if i*2 < int(width*height) {
			charIndices[i*2] = b >> 4
		}
		if i*2+1 < int(width*height) {
			charIndices[i*2+1] = b & 0x0F
		}
	}

	// Read packed edge flags
	edgeFlagsSize := (int(colorWidth*colorHeight) + 3) / 4
	packedEdgeFlags := make([]byte, edgeFlagsSize)
	if _, err := io.ReadFull(zr, packedEdgeFlags); err != nil {
		return nil, fmt.Errorf("failed to read packed edge flags from zstd: %w", err)
	}

	// Unpack edge flags
	edgeFlags := make([]byte, colorWidth*colorHeight)
	for i := 0; i < int(colorWidth*colorHeight); i++ {
		edgeFlags[i] = (packedEdgeFlags[i/4] >> ((i % 4) * 2)) & 0x03
	}

	// Read RLE compressed color indices (remaining stream data)
	rleColorIndices, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("failed to read RLE color indices from zstd: %w", err)
	}

	// Decode RLE
	colorIndices, err := DecodeRLE(rleColorIndices)
	if err != nil {
		return nil, fmt.Errorf("failed to decode RLE color indices: %w", err)
	}

	expectedColorSize := int(colorWidth * colorHeight)
	if len(colorIndices) != expectedColorSize {
		return nil, fmt.Errorf("color grid size mismatch: expected %d, got %d", expectedColorSize, len(colorIndices))
	}

	// Reconstruct cells by upscaling color channels using 2D bilinear interpolation
	gridSize := int(width * height)
	cells := make([]Cell, gridSize)
	paletteLen := len(Palette)

	for y := 0; y < int(height); y++ {
		for x := 0; x < int(width); x++ {
			charIdx := charIndices[y*int(width)+x]
			if int(charIdx) >= paletteLen {
				charIdx = 0
			}
			char := Palette[charIdx]

			// Calculate low-res source coordinates (grid aligned)
			srcX := 0.0
			if width > 1 {
				srcX = float64(x) * float64(colorWidth-1) / float64(width-1)
			}
			srcY := 0.0
			if height > 1 {
				srcY = float64(y) * float64(colorHeight-1) / float64(height-1)
			}

			x0 := int(math.Floor(srcX))
			y0 := int(math.Floor(srcY))
			x1 := x0 + 1
			y1 := y0 + 1

			if x1 >= int(colorWidth) {
				x1 = int(colorWidth) - 1
			}
			if y1 >= int(colorHeight) {
				y1 = int(colorHeight) - 1
			}

			dx := srcX - float64(x0)
			dy := srcY - float64(y0)

			c00 := palette[colorIndices[y0*int(colorWidth)+x0]]
			c10 := palette[colorIndices[y0*int(colorWidth)+x1]]
			c01 := palette[colorIndices[y1*int(colorWidth)+x0]]
			c11 := palette[colorIndices[y1*int(colorWidth)+x1]]

			// Helper to get neighbor's char index
			getNeighCharIdx := func(lx, ly int) byte {
				hx := lx * int(width) / int(colorWidth)
				hy := ly * int(height) / int(colorHeight)
				if hx >= int(width) {
					hx = int(width) - 1
				}
				if hy >= int(height) {
					hy = int(height) - 1
				}
				return charIndices[hy*int(width)+hx]
			}

			// Helper to get similarity weight
			getSimWeight := func(neighCharIdx byte) float64 {
				diff := int(charIdx) - int(neighCharIdx)
				if diff < 0 {
					diff = -diff
				}
				if diff > 2 {
					return 0.05
				}
				return 1.0
			}

			w00 := (1.0 - dx) * (1.0 - dy) * getSimWeight(getNeighCharIdx(x0, y0))
			w10 := dx * (1.0 - dy) * getSimWeight(getNeighCharIdx(x1, y0))
			w01 := (1.0 - dx) * dy * getSimWeight(getNeighCharIdx(x0, y1))
			w11 := dx * dy * getSimWeight(getNeighCharIdx(x1, y1))

			// Apply edge-guided direction flags
			closestX := x0
			if dx >= 0.5 {
				closestX = x1
			}
			closestY := y0
			if dy >= 0.5 {
				closestY = y1
			}
			edgeFlag := edgeFlags[closestY*int(colorWidth)+closestX]

			if edgeFlag == 1 { // Vertical Edge (separating left and right)
				leftChar := getNeighCharIdx(x0, closestY)
				rightChar := getNeighCharIdx(x1, closestY)
				if absDiff(charIdx, leftChar) < absDiff(charIdx, rightChar) {
					w10 *= 0.05
					w11 *= 0.05
				} else {
					w00 *= 0.05
					w01 *= 0.05
				}
			} else if edgeFlag == 2 { // Horizontal Edge (separating top and bottom)
				topChar := getNeighCharIdx(closestX, y0)
				bottomChar := getNeighCharIdx(closestX, y1)
				if absDiff(charIdx, topChar) < absDiff(charIdx, bottomChar) {
					w01 *= 0.05
					w11 *= 0.05
				} else {
					w00 *= 0.05
					w10 *= 0.05
				}
			}

			wSum := w00 + w10 + w01 + w11

			var r, g, b uint8
			if wSum > 0.01 {
				r = uint8(math.Round((float64(c00.R)*w00 + float64(c10.R)*w10 + float64(c01.R)*w01 + float64(c11.R)*w11) / wSum))
				g = uint8(math.Round((float64(c00.G)*w00 + float64(c10.G)*w10 + float64(c01.G)*w01 + float64(c11.G)*w11) / wSum))
				b = uint8(math.Round((float64(c00.B)*w00 + float64(c10.B)*w10 + float64(c01.B)*w01 + float64(c11.B)*w11) / wSum))
			} else {
				// Fallback to standard bilinear
				r = uint8(math.Round((1.0-dx)*(1.0-dy)*float64(c00.R) + dx*(1.0-dy)*float64(c10.R) + (1.0-dx)*dy*float64(c01.R) + dx*dy*float64(c11.R)))
				g = uint8(math.Round((1.0-dx)*(1.0-dy)*float64(c00.G) + dx*(1.0-dy)*float64(c10.G) + (1.0-dx)*dy*float64(c01.G) + dx*dy*float64(c11.G)))
				b = uint8(math.Round((1.0-dx)*(1.0-dy)*float64(c00.B) + dx*(1.0-dy)*float64(c10.B) + (1.0-dx)*dy*float64(c11.B)))
			}

			cells[y*int(width)+x] = Cell{
				Char: char,
				R:    r,
				G:    g,
				B:    b,
			}
		}
	}

	return &Art{
		Width:      int(width),
		Height:     int(height),
		OrigWidth:  int(origWidth),
		OrigHeight: int(origHeight),
		Cells:      cells,
	}, nil
}

func getPaletteIndex(char rune) byte {
	for i, r := range Palette {
		if r == char {
			return byte(i)
		}
	}
	return 0
}

func interpolateColors(c00, c10, c01, c11, dx, dy float64) float64 {
	return (1-dx)*(1-dy)*c00 + dx*(1-dy)*c10 + (1-dx)*dy*c01 + dx*dy*c11
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

func absDiff(a, b byte) int {
	diff := int(a) - int(b)
	if diff < 0 {
		return -diff
	}
	return diff
}
