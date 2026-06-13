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

// SaveGAC writes the Art to a file using 256-color palette quantization and Zstd compression.
func SaveGAC(art *Art, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// 1. Run color quantization
	palette, indices := Quantize(art)
	paletteSize := len(palette)

	// Write magic
	if _, err := file.WriteString(Magic); err != nil {
		return fmt.Errorf("failed to write magic: %w", err)
	}

	// Write width, height, original width, original height, and palette size (24-byte header)
	if err := binary.Write(file, binary.BigEndian, uint32(art.Width)); err != nil {
		return fmt.Errorf("failed to write width: %w", err)
	}
	if err := binary.Write(file, binary.BigEndian, uint32(art.Height)); err != nil {
		return fmt.Errorf("failed to write height: %w", err)
	}
	if err := binary.Write(file, binary.BigEndian, uint32(art.OrigWidth)); err != nil {
		return fmt.Errorf("failed to write original width: %w", err)
	}
	if err := binary.Write(file, binary.BigEndian, uint32(art.OrigHeight)); err != nil {
		return fmt.Errorf("failed to write original height: %w", err)
	}
	if err := binary.Write(file, binary.BigEndian, uint32(paletteSize)); err != nil {
		return fmt.Errorf("failed to write palette size: %w", err)
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

	// Write index grid: Width * Height bytes
	if _, err := zw.Write(indices); err != nil {
		return fmt.Errorf("failed to write indices to zstd: %w", err)
	}

	return nil
}

// LoadGAC reads an Art from a .gac file, decompressing the palette and index grid.
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

	// Read width, height, original width, original height, and palette size
	var width, height, origWidth, origHeight, paletteSize uint32
	if err := binary.Read(file, binary.BigEndian, &width); err != nil {
		return nil, fmt.Errorf("failed to read width: %w", err)
	}
	if err := binary.Read(file, binary.BigEndian, &height); err != nil {
		return nil, fmt.Errorf("failed to read height: %w", err)
	}
	if err := binary.Read(file, binary.BigEndian, &origWidth); err != nil {
		return nil, fmt.Errorf("failed to read original width: %w", err)
	}
	if err := binary.Read(file, binary.BigEndian, &origHeight); err != nil {
		return nil, fmt.Errorf("failed to read original height: %w", err)
	}
	if err := binary.Read(file, binary.BigEndian, &paletteSize); err != nil {
		return nil, fmt.Errorf("failed to read palette size: %w", err)
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

	// Read index grid
	gridSize := int(width * height)
	indices := make([]byte, gridSize)
	if _, err := io.ReadFull(zr, indices); err != nil {
		return nil, fmt.Errorf("failed to read indices from zstd: %w", err)
	}

	// Reconstruct cells
	cells := make([]Cell, gridSize)
	paletteLen := len(Palette)

	for i := 0; i < gridSize; i++ {
		idx := indices[i]
		if int(idx) >= len(palette) {
			return nil, fmt.Errorf("index out of palette bounds: got %d, palette size %d", idx, len(palette))
		}
		c := palette[idx]

		// Calculate brightness dynamically
		lum := 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)

		// Map brightness to palette character
		charIdx := int(math.Round(lum * float64(paletteLen-1) / 255.0))
		if charIdx < 0 {
			charIdx = 0
		} else if charIdx >= paletteLen {
			charIdx = paletteLen - 1
		}

		cells[i] = Cell{
			Char: Palette[charIdx],
			R:    c.R,
			G:    c.G,
			B:    c.B,
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
