package ascii

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/klauspost/compress/zstd"
)

// Magic header bytes for .gac files.
const Magic = "GASC"

// SaveGAC writes the Art to a file using the custom compressed format (RGB colors only, Zstd-compressed).
func SaveGAC(art *Art, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Write magic
	if _, err := file.WriteString(Magic); err != nil {
		return fmt.Errorf("failed to write magic: %w", err)
	}

	// Write width, height, original width, original height
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

	// Create zstd writer
	zw, err := zstd.NewWriter(file)
	if err != nil {
		return fmt.Errorf("failed to create zstd writer: %w", err)
	}
	defer zw.Close()

	// Write RGB colors (sequential R, G, B values)
	colors := make([]byte, len(art.Cells)*3)
	for i, cell := range art.Cells {
		colors[i*3] = cell.R
		colors[i*3+1] = cell.G
		colors[i*3+2] = cell.B
	}
	if _, err := zw.Write(colors); err != nil {
		return fmt.Errorf("failed to write colors to zstd: %w", err)
	}

	return nil
}

// LoadGAC reads an Art from a .gac file (decompressing RGB colors with Zstd, computing ASCII characters dynamically).
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

	// Read width, height, original width, original height
	var width, height, origWidth, origHeight uint32
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

	// Create zstd reader
	zr, err := zstd.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd reader: %w", err)
	}
	defer zr.Close()

	size := int(width * height)

	// Read RGB colors
	colors := make([]byte, size*3)
	if _, err := io.ReadFull(zr, colors); err != nil {
		return nil, fmt.Errorf("failed to read colors from zstd: %w", err)
	}

	cells := make([]Cell, size)
	paletteLen := len(Palette)

	for i := 0; i < size; i++ {
		r := colors[i*3]
		g := colors[i*3+1]
		b := colors[i*3+2]

		// Calculate brightness dynamically
		lum := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)

		// Map brightness to palette character
		idx := int(math.Round(lum * float64(paletteLen-1) / 255.0))
		if idx < 0 {
			idx = 0
		} else if idx >= paletteLen {
			idx = paletteLen - 1
		}

		cells[i] = Cell{
			Char: Palette[idx],
			R:    r,
			G:    g,
			B:    b,
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
