package ascii

import (
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// Magic header bytes for .gac files.
const Magic = "GASC"

// SaveGAC writes the Art to a file using the custom compressed format.
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

	// Write width and height
	if err := binary.Write(file, binary.BigEndian, uint32(art.Width)); err != nil {
		return fmt.Errorf("failed to write width: %w", err)
	}
	if err := binary.Write(file, binary.BigEndian, uint32(art.Height)); err != nil {
		return fmt.Errorf("failed to write height: %w", err)
	}

	// Create zlib writer
	zw := zlib.NewWriter(file)
	defer zw.Close()

	// Write characters
	chars := make([]byte, len(art.Cells))
	for i, cell := range art.Cells {
		chars[i] = byte(cell.Char)
	}
	if _, err := zw.Write(chars); err != nil {
		return fmt.Errorf("failed to write characters to zlib: %w", err)
	}

	// Write RGB colors
	colors := make([]byte, len(art.Cells)*3)
	for i, cell := range art.Cells {
		colors[i*3] = cell.R
		colors[i*3+1] = cell.G
		colors[i*3+2] = cell.B
	}
	if _, err := zw.Write(colors); err != nil {
		return fmt.Errorf("failed to write colors to zlib: %w", err)
	}

	return nil
}

// LoadGAC reads an Art from a .gac file.
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

	// Read width and height
	var width, height uint32
	if err := binary.Read(file, binary.BigEndian, &width); err != nil {
		return nil, fmt.Errorf("failed to read width: %w", err)
	}
	if err := binary.Read(file, binary.BigEndian, &height); err != nil {
		return nil, fmt.Errorf("failed to read height: %w", err)
	}

	// Create zlib reader
	zr, err := zlib.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to create zlib reader: %w", err)
	}
	defer zr.Close()

	size := int(width * height)

	// Read characters
	chars := make([]byte, size)
	if _, err := io.ReadFull(zr, chars); err != nil {
		return nil, fmt.Errorf("failed to read characters from zlib: %w", err)
	}

	// Read RGB colors
	colors := make([]byte, size*3)
	if _, err := io.ReadFull(zr, colors); err != nil {
		return nil, fmt.Errorf("failed to read colors from zlib: %w", err)
	}

	cells := make([]Cell, size)
	for i := 0; i < size; i++ {
		cells[i] = Cell{
			Char: rune(chars[i]),
			R:    colors[i*3],
			G:    colors[i*3+1],
			B:    colors[i*3+2],
		}
	}

	return &Art{
		Width:  int(width),
		Height: int(height),
		Cells:  cells,
	}, nil
}
