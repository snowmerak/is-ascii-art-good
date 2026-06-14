package ascii

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// GAVMagic is the header magic bytes for .gav files.
const GAVMagic = "GAVS"

// PlayVideo reads and plays a .gav file in the terminal.
func PlayVideo(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open video: %w", err)
	}
	defer file.Close()

	// Read magic
	magic := make([]byte, 4)
	if _, err := io.ReadFull(file, magic); err != nil {
		return fmt.Errorf("failed to read magic: %w", err)
	}
	if string(magic) != GAVMagic {
		return fmt.Errorf("invalid video format: expected %s, got %s", GAVMagic, string(magic))
	}

	// Read header
	var width, height, colorWidth, colorHeight, fps, frameCount uint32
	fields := []*uint32{&width, &height, &colorWidth, &colorHeight, &fps, &frameCount}
	for _, ptr := range fields {
		if err := binary.Read(file, binary.BigEndian, ptr); err != nil {
			return fmt.Errorf("failed to read header field: %w", err)
		}
	}

	// Read global palette size
	var paletteSize uint32
	if err := binary.Read(file, binary.BigEndian, &paletteSize); err != nil {
		return fmt.Errorf("failed to read palette size: %w", err)
	}

	// Read global palette
	palBytes := make([]byte, paletteSize*3)
	if _, err := io.ReadFull(file, palBytes); err != nil {
		return fmt.Errorf("failed to read global palette: %w", err)
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

	// Prepare zstd reader
	zr, err := zstd.NewReader(nil)
	if err != nil {
		return fmt.Errorf("failed to initialize zstd decoder: %w", err)
	}
	defer zr.Close()

	// State for decoding
	charGrid := make([]byte, width*height)
	colorIndices := make([]byte, colorWidth*colorHeight)
	edgeFlags := make([]byte, colorWidth*colorHeight)

	// Hide cursor and clear screen once
	fmt.Print("\x1b[?25l\x1b[2J\x1b[H")
	defer fmt.Print("\x1b[?25h\x1b[0m\n")

	frameInterval := time.Second / time.Duration(fps)
	startTime := time.Now()

	for f := 0; f < int(frameCount); f++ {
		frameStartTime := time.Now()

		// Read Frame Header
		var frameType byte
		var payloadSize uint32
		if err := binary.Read(file, binary.BigEndian, &frameType); err != nil {
			return fmt.Errorf("failed to read frame type at frame %d: %w", f, err)
		}
		if err := binary.Read(file, binary.BigEndian, &payloadSize); err != nil {
			return fmt.Errorf("failed to read payload size at frame %d: %w", f, err)
		}

		payload := make([]byte, payloadSize)
		if _, err := io.ReadFull(file, payload); err != nil {
			return fmt.Errorf("failed to read frame payload at frame %d: %w", f, err)
		}

		// Decompress payload
		decompressed, err := zr.DecodeAll(payload, nil)
		if err != nil {
			return fmt.Errorf("failed to decompress frame %d: %w", f, err)
		}

		buf := bytes.NewReader(decompressed)

		if frameType == 0 { // I-Frame
			var rleCharsLen uint32
			if err := binary.Read(buf, binary.BigEndian, &rleCharsLen); err != nil {
				return fmt.Errorf("failed to read rle chars len in I-frame %d: %w", f, err)
			}
			rleChars := make([]byte, rleCharsLen)
			if _, err := io.ReadFull(buf, rleChars); err != nil {
				return fmt.Errorf("failed to read rle chars in I-frame %d: %w", f, err)
			}
			decodedChars, err := DecodeRLE(rleChars)
			if err != nil {
				return fmt.Errorf("failed to decode RLE chars in I-frame %d: %w", f, err)
			}
			copy(charGrid, decodedChars)

			// Read RLE color indices
			rleColors := make([]byte, buf.Len())
			if _, err := io.ReadFull(buf, rleColors); err != nil {
				return fmt.Errorf("failed to read RLE colors in I-frame %d: %w", f, err)
			}
			decodedColors, err := DecodeRLE(rleColors)
			if err != nil {
				return fmt.Errorf("failed to decode RLE in I-frame %d: %w", f, err)
			}
			copy(colorIndices, decodedColors)

		} else { // P-Frame
			// Read char mask
			charMaskSize := (int(width*height) + 7) / 8
			charMask := make([]byte, charMaskSize)
			if _, err := io.ReadFull(buf, charMask); err != nil {
				return fmt.Errorf("failed to read char mask in P-frame %d: %w", f, err)
			}

			numChangedChars := 0
			for _, b := range charMask {
				for bit := 0; bit < 8; bit++ {
					if (b & (1 << bit)) != 0 {
						numChangedChars++
					}
				}
			}

			// Read packed changed characters
			packedChangedCharSize := (numChangedChars + 1) / 2
			packedChanged := make([]byte, packedChangedCharSize)
			if _, err := io.ReadFull(buf, packedChanged); err != nil {
				return fmt.Errorf("failed to read packed changed chars in P-frame %d: %w", f, err)
			}

			changedIdx := 0
			for i := 0; i < int(width*height); i++ {
				byteIdx := i / 8
				bitIdx := i % 8
				if (charMask[byteIdx] & (1 << bitIdx)) != 0 {
					b := packedChanged[changedIdx/2]
					var val byte
					if changedIdx%2 == 0 {
						val = b >> 4
					} else {
						val = b & 0x0F
					}
					charGrid[i] = val
					changedIdx++
				}
			}

			// Read color mask
			colorMaskSize := (int(colorWidth*colorHeight) + 7) / 8
			colorMask := make([]byte, colorMaskSize)
			if _, err := io.ReadFull(buf, colorMask); err != nil {
				return fmt.Errorf("failed to read color mask in P-frame %d: %w", f, err)
			}

			numChangedColors := 0
			for _, b := range colorMask {
				for bit := 0; bit < 8; bit++ {
					if (b & (1 << bit)) != 0 {
						numChangedColors++
					}
				}
			}

			// Read changed colors (packed 4-bit if paletteSize <= 16)
			var changedColors []byte
			if paletteSize <= 16 {
				packedColorSize := (numChangedColors + 1) / 2
				packedColors := make([]byte, packedColorSize)
				if _, err := io.ReadFull(buf, packedColors); err != nil {
					return fmt.Errorf("failed to read packed changed colors in P-frame %d: %w", f, err)
				}
				changedColors = make([]byte, numChangedColors)
				for i := 0; i < numChangedColors; i++ {
					b := packedColors[i/2]
					if i%2 == 0 {
						changedColors[i] = b >> 4
					} else {
						changedColors[i] = b & 0x0F
					}
				}
			} else {
				changedColors = make([]byte, numChangedColors)
				if _, err := io.ReadFull(buf, changedColors); err != nil {
					return fmt.Errorf("failed to read changed colors in P-frame %d: %w", f, err)
				}
			}

			changedColIdx := 0
			for i := 0; i < int(colorWidth*colorHeight); i++ {
				byteIdx := i / 8
				bitIdx := i % 8
				if (colorMask[byteIdx] & (1 << bitIdx)) != 0 {
					colorIndices[i] = changedColors[changedColIdx]
					changedColIdx++
				}
			}
		}

		// Compute edge flags dynamically
		for cy := 0; cy < int(colorHeight); cy++ {
			for cx := 0; cx < int(colorWidth); cx++ {
				xStart := cx * int(width) / int(colorWidth)
				xEnd := (cx + 1) * int(width) / int(colorWidth)
				yStart := cy * int(height) / int(colorHeight)
				yEnd := (cy + 1) * int(height) / int(colorHeight)

				var d00, d10, d01, d11 byte
				d00 = charGrid[yStart*int(width)+xStart]

				xRight := xEnd - 1
				if xRight < xStart {
					xRight = xStart
				}
				yBottom := yEnd - 1
				if yBottom < yStart {
					yBottom = yStart
				}

				d10 = charGrid[yStart*int(width)+xRight]
				d01 = charGrid[yBottom*int(width)+xStart]
				d11 = charGrid[yBottom*int(width)+xRight]

				dh := absDiff(d00, d10) + absDiff(d01, d11)
				dv := absDiff(d00, d01) + absDiff(d10, d11)

				var flag byte = 0
				if dh > 2 || dv > 2 {
					if dh > dv {
						flag = 1
					} else if dv > dh {
						flag = 2
					} else {
						flag = 3
					}
				}
				edgeFlags[cy*int(colorWidth)+cx] = flag
			}
		}

		// Reconstruct and print directly
		fmt.Print("\x1b[H") // move cursor home

		paletteLen := len(Palette)
		var curR, curG, curB uint8
		firstColor := true

		var lineBuf strings.Builder
		lineBuf.Grow(int(width)*25 + int(height))

		for y := 0; y < int(height); y++ {
			for x := 0; x < int(width); x++ {
				charIdx := charGrid[y*int(width)+x]
				if int(charIdx) >= paletteLen {
					charIdx = 0
				}
				char := Palette[charIdx]

				// Bilateral upscaling logic
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

				getNeighCharIdx := func(lx, ly int) byte {
					hx := lx * int(width) / int(colorWidth)
					hy := ly * int(height) / int(colorHeight)
					if hx >= int(width) {
						hx = int(width) - 1
					}
					if hy >= int(height) {
						hy = int(height) - 1
					}
					return charGrid[hy*int(width)+hx]
				}

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

				closestX := x0
				if dx >= 0.5 {
					closestX = x1
				}
				closestY := y0
				if dy >= 0.5 {
					closestY = y1
				}
				edgeFlag := edgeFlags[closestY*int(colorWidth)+closestX]

				if edgeFlag == 1 {
					leftChar := getNeighCharIdx(x0, closestY)
					rightChar := getNeighCharIdx(x1, closestY)
					if absDiff(charIdx, leftChar) < absDiff(charIdx, rightChar) {
						w10 *= 0.05
						w11 *= 0.05
					} else {
						w00 *= 0.05
						w01 *= 0.05
					}
				} else if edgeFlag == 2 {
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
					r = uint8(math.Round((1.0-dx)*(1.0-dy)*float64(c00.R) + dx*(1.0-dy)*float64(c10.R) + (1.0-dx)*dy*float64(c01.R) + dx*dy*float64(c11.R)))
					g = uint8(math.Round((1.0-dx)*(1.0-dy)*float64(c00.G) + dx*(1.0-dy)*float64(c10.G) + (1.0-dx)*dy*float64(c01.G) + dx*dy*float64(c11.G)))
					b = uint8(math.Round((1.0-dx)*(1.0-dy)*float64(c00.B) + dx*(1.0-dy)*float64(c10.B) + (1.0-dx)*dy*float64(c11.B)))
				}

				if firstColor || r != curR || g != curG || b != curB {
					lineBuf.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b))
					curR, curG, curB = r, g, b
					firstColor = false
				}
				lineBuf.WriteRune(char)
			}
			lineBuf.WriteString("\x1b[0m\n")
			firstColor = true
		}
		fmt.Print(lineBuf.String())

		// Timing control
		elapsed := time.Since(frameStartTime)
		if elapsed < frameInterval {
			time.Sleep(frameInterval - elapsed)
		}
	}

	totalElapsed := time.Since(startTime)
	fmt.Printf("\x1b[0m\nPlayback Finished! Rendered %d frames in %v (Average FPS: %.1f)\n", frameCount, totalElapsed, float64(frameCount)/totalElapsed.Seconds())
	return nil
}

// CompressVideo reads images from a directory, compresses them into .gav format.
func CompressVideo(framesDir, outputPath string, targetWidth int, fps int, colorScale int) error {
	entries, err := os.ReadDir(framesDir)
	if err != nil {
		return fmt.Errorf("failed to read frames directory: %w", err)
	}

	var filePaths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") {
			filePaths = append(filePaths, filepath.Join(framesDir, entry.Name()))
		}
	}
	sort.Strings(filePaths)

	if len(filePaths) == 0 {
		return fmt.Errorf("no image frames found in directory %s", framesDir)
	}

	fmt.Printf("Found %d frames. Starting preprocessing...\n", len(filePaths))

	// 1. Process first frame to establish dimensions
	firstImg, err := LoadImage(filePaths[0])
	if err != nil {
		return fmt.Errorf("failed to load first frame: %w", err)
	}

	origW := firstImg.Bounds().Dx()
	origH := firstImg.Bounds().Dy()

	var actualTargetW int
	if targetWidth <= 0 {
		actualTargetW = origW
	} else {
		actualTargetW = targetWidth
	}

	// Calculate aspect ratio for resizing
	var resizedFirst image.Image
	if targetWidth <= 0 {
		resizedFirst = firstImg
	} else {
		resizedFirst = ResizeBilinear(firstImg, actualTargetW, 0.5)
	}

	width := uint32(resizedFirst.Bounds().Dx())
	height := uint32(resizedFirst.Bounds().Dy())

	if colorScale < 1 {
		colorScale = 1
	}

	colorWidth := width / uint32(colorScale)
	if colorWidth < 1 {
		colorWidth = 1
	}
	colorHeight := height / uint32(colorScale)
	if colorHeight < 1 {
		colorHeight = 1
	}

	// 2. Process all frames into Art grids and gather all colors for global palette
	arts := make([]*Art, len(filePaths))
	allColors := make([]color.RGBA, len(filePaths)*int(colorWidth*colorHeight))

	for i, path := range filePaths {
		img, err := LoadImage(path)
		if err != nil {
			return fmt.Errorf("failed to load frame %d (%s): %w", i, filepath.Base(path), err)
		}

		var resized image.Image
		if targetWidth <= 0 {
			resized = img
		} else {
			resized = ResizeBilinear(img, int(width), 0.5)
		}

		art := ConvertToASCII(resized, origW, origH)
		arts[i] = art

		// Generate downscaled color grid
		for cy := 0; cy < int(colorHeight); cy++ {
			for cx := 0; cx < int(colorWidth); cx++ {
				xStart := cx * int(width) / int(colorWidth)
				xEnd := (cx + 1) * int(width) / int(colorWidth)
				yStart := cy * int(height) / int(colorHeight)
				yEnd := (cy + 1) * int(height) / int(colorHeight)

				var sumR, sumG, sumB int
				count := 0
				for y := yStart; y < yEnd; y++ {
					for x := xStart; x < xEnd; x++ {
						cell := art.Cells[y*int(width)+x]
						sumR += int(cell.R)
						sumG += int(cell.G)
						sumB += int(cell.B)
						count++
					}
				}
				col := color.RGBA{R: 0, G: 0, B: 0, A: 255}
				if count > 0 {
					col.R = uint8(sumR / count)
					col.G = uint8(sumG / count)
					col.B = uint8(sumB / count)
				}
				allColors[i*int(colorWidth*colorHeight)+cy*int(colorWidth)+cx] = col
			}
		}
	}

	// 3. Perform global color quantization
	globalPalette, allColorIndices := QuantizeColors(allColors)
	paletteSize := len(globalPalette)

	// 4. Prepare output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	// Write magic
	if _, err := outFile.WriteString(GAVMagic); err != nil {
		return fmt.Errorf("failed to write magic: %w", err)
	}

	// Write Global Header
	headerFields := []uint32{width, height, colorWidth, colorHeight, uint32(fps), uint32(len(filePaths))}
	for _, val := range headerFields {
		if err := binary.Write(outFile, binary.BigEndian, val); err != nil {
			return fmt.Errorf("failed to write header field: %w", err)
		}
	}

	// Write global palette size
	if err := binary.Write(outFile, binary.BigEndian, uint32(paletteSize)); err != nil {
		return fmt.Errorf("failed to write palette size: %w", err)
	}

	// Write global palette
	palBytes := make([]byte, paletteSize*3)
	for i, c := range globalPalette {
		palBytes[i*3] = c.R
		palBytes[i*3+1] = c.G
		palBytes[i*3+2] = c.B
	}
	if _, err := outFile.Write(palBytes); err != nil {
		return fmt.Errorf("failed to write global palette: %w", err)
	}

	// Setup zstd encoder
	zw, err := zstd.NewWriter(nil)
	if err != nil {
		return fmt.Errorf("failed to create zstd writer: %w", err)
	}
	defer zw.Close()

	// 5. Compress and write frames
	var prevCharGrid []byte
	var prevColorIndices []byte

	for f := 0; f < len(filePaths); f++ {
		art := arts[f]
		colorIndices := allColorIndices[f*int(colorWidth*colorHeight) : (f+1)*int(colorWidth*colorHeight)]

		// Extract characters to density indices
		charGrid := make([]byte, width*height)
		for i := 0; i < int(width*height); i++ {
			charGrid[i] = getPaletteIndex(art.Cells[i].Char)
		}

		var frameType byte
		var framePayload []byte

		// Decide keyframe every 60 frames or as first frame
		isKeyframe := f == 0 || f%60 == 0

		if isKeyframe {
			frameType = 0 // I-Frame

			var rawBuf bytes.Buffer

			// RLE chars
			rleChars := EncodeRLE(charGrid)
			if err := binary.Write(&rawBuf, binary.BigEndian, uint32(len(rleChars))); err != nil {
				return fmt.Errorf("failed to write rle chars len: %w", err)
			}
			rawBuf.Write(rleChars)

			// RLE color indices
			rleColors := EncodeRLE(colorIndices)
			rawBuf.Write(rleColors)

			// Compress payload
			framePayload = zw.EncodeAll(rawBuf.Bytes(), nil)

		} else {
			frameType = 1 // P-Frame

			var rawBuf bytes.Buffer

			// Calculate character difference mask and changes
			charMask := make([]byte, (int(width*height)+7)/8)
			var changedChars []byte
			for i := 0; i < int(width*height); i++ {
				if charGrid[i] != prevCharGrid[i] {
					byteIdx := i / 8
					bitIdx := i % 8
					charMask[byteIdx] |= (1 << bitIdx)
					changedChars = append(changedChars, charGrid[i])
				}
			}
			rawBuf.Write(charMask)

			// Pack changed characters (4-bit)
			packedChanged := make([]byte, (len(changedChars)+1)/2)
			for i := 0; i < len(changedChars); i += 2 {
				val1 := changedChars[i]
				var val2 byte = 0
				if i+1 < len(changedChars) {
					val2 = changedChars[i+1]
				}
				packedChanged[i/2] = (val1 << 4) | (val2 & 0x0F)
			}
			rawBuf.Write(packedChanged)

			// Calculate color difference mask and changes
			colorMask := make([]byte, (int(colorWidth*colorHeight)+7)/8)
			var changedColors []byte
			for i := 0; i < int(colorWidth*colorHeight); i++ {
				if colorIndices[i] != prevColorIndices[i] {
					byteIdx := i / 8
					bitIdx := i % 8
					colorMask[byteIdx] |= (1 << bitIdx)
					changedColors = append(changedColors, colorIndices[i])
				}
			}
			rawBuf.Write(colorMask)

			// Write changed colors (packed 4-bit if paletteSize <= 16)
			if paletteSize <= 16 {
				packed := make([]byte, (len(changedColors)+1)/2)
				for i := 0; i < len(changedColors); i += 2 {
					val1 := changedColors[i]
					var val2 byte = 0
					if i+1 < len(changedColors) {
						val2 = changedColors[i+1]
					}
					packed[i/2] = (val1 << 4) | (val2 & 0x0F)
				}
				rawBuf.Write(packed)
			} else {
				rawBuf.Write(changedColors)
			}

			// Compress payload
			framePayload = zw.EncodeAll(rawBuf.Bytes(), nil)
		}

		// Write frame header
		if err := binary.Write(outFile, binary.BigEndian, frameType); err != nil {
			return fmt.Errorf("failed to write frame type for frame %d: %w", f, err)
		}
		if err := binary.Write(outFile, binary.BigEndian, uint32(len(framePayload))); err != nil {
			return fmt.Errorf("failed to write payload size for frame %d: %w", f, err)
		}

		// Write compressed frame payload
		if _, err := outFile.Write(framePayload); err != nil {
			return fmt.Errorf("failed to write frame payload for frame %d: %w", f, err)
		}

		prevCharGrid = charGrid
		prevColorIndices = colorIndices
	}

	return nil
}

// ExportVideo reads a .gav file and exports all reconstructed frames as PNGs.
func ExportVideo(inputPath, outputDir, mode string) error {
	file, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("failed to open video: %w", err)
	}
	defer file.Close()

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Read magic
	magic := make([]byte, 4)
	if _, err := io.ReadFull(file, magic); err != nil {
		return fmt.Errorf("failed to read magic: %w", err)
	}
	if string(magic) != GAVMagic {
		return fmt.Errorf("invalid video format: expected %s, got %s", GAVMagic, string(magic))
	}

	// Read header
	var width, height, colorWidth, colorHeight, fps, frameCount uint32
	fields := []*uint32{&width, &height, &colorWidth, &colorHeight, &fps, &frameCount}
	for _, ptr := range fields {
		if err := binary.Read(file, binary.BigEndian, ptr); err != nil {
			return fmt.Errorf("failed to read header field: %w", err)
		}
	}

	// Read global palette size
	var paletteSize uint32
	if err := binary.Read(file, binary.BigEndian, &paletteSize); err != nil {
		return fmt.Errorf("failed to read palette size: %w", err)
	}

	// Read global palette
	palBytes := make([]byte, paletteSize*3)
	if _, err := io.ReadFull(file, palBytes); err != nil {
		return fmt.Errorf("failed to read global palette: %w", err)
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

	// Prepare zstd reader
	zr, err := zstd.NewReader(nil)
	if err != nil {
		return fmt.Errorf("failed to initialize zstd decoder: %w", err)
	}
	defer zr.Close()

	charGrid := make([]byte, width*height)
	colorIndices := make([]byte, colorWidth*colorHeight)
	edgeFlags := make([]byte, colorWidth*colorHeight)

	fmt.Printf("Exporting %d frames to %s...\n", frameCount, outputDir)

	for f := 0; f < int(frameCount); f++ {
		var frameType byte
		var payloadSize uint32
		if err := binary.Read(file, binary.BigEndian, &frameType); err != nil {
			return fmt.Errorf("failed to read frame type at frame %d: %w", f, err)
		}
		if err := binary.Read(file, binary.BigEndian, &payloadSize); err != nil {
			return fmt.Errorf("failed to read payload size at frame %d: %w", f, err)
		}

		payload := make([]byte, payloadSize)
		if _, err := io.ReadFull(file, payload); err != nil {
			return fmt.Errorf("failed to read frame payload at frame %d: %w", f, err)
		}

		decompressed, err := zr.DecodeAll(payload, nil)
		if err != nil {
			return fmt.Errorf("failed to decompress frame %d: %w", f, err)
		}

		buf := bytes.NewReader(decompressed)

		if frameType == 0 { // I-Frame
			var rleCharsLen uint32
			if err := binary.Read(buf, binary.BigEndian, &rleCharsLen); err != nil {
				return fmt.Errorf("failed to read rle chars len in I-frame %d: %w", f, err)
			}
			rleChars := make([]byte, rleCharsLen)
			if _, err := io.ReadFull(buf, rleChars); err != nil {
				return fmt.Errorf("failed to read rle chars in I-frame %d: %w", f, err)
			}
			decodedChars, err := DecodeRLE(rleChars)
			if err != nil {
				return fmt.Errorf("failed to decode RLE chars in I-frame %d: %w", f, err)
			}
			copy(charGrid, decodedChars)

			// Read RLE color indices
			rleColors := make([]byte, buf.Len())
			if _, err := io.ReadFull(buf, rleColors); err != nil {
				return fmt.Errorf("failed to read RLE colors in I-frame %d: %w", f, err)
			}
			decodedColors, err := DecodeRLE(rleColors)
			if err != nil {
				return fmt.Errorf("failed to decode RLE in I-frame %d: %w", f, err)
			}
			copy(colorIndices, decodedColors)

		} else { // P-Frame
			// Read char mask
			charMaskSize := (int(width*height) + 7) / 8
			charMask := make([]byte, charMaskSize)
			if _, err := io.ReadFull(buf, charMask); err != nil {
				return fmt.Errorf("failed to read char mask in P-frame %d: %w", f, err)
			}

			numChangedChars := 0
			for _, b := range charMask {
				for bit := 0; bit < 8; bit++ {
					if (b & (1 << bit)) != 0 {
						numChangedChars++
					}
				}
			}

			// Read packed changed characters
			packedChangedCharSize := (numChangedChars + 1) / 2
			packedChanged := make([]byte, packedChangedCharSize)
			if _, err := io.ReadFull(buf, packedChanged); err != nil {
				return fmt.Errorf("failed to read packed changed chars in P-frame %d: %w", f, err)
			}

			changedIdx := 0
			for i := 0; i < int(width*height); i++ {
				byteIdx := i / 8
				bitIdx := i % 8
				if (charMask[byteIdx] & (1 << bitIdx)) != 0 {
					b := packedChanged[changedIdx/2]
					var val byte
					if changedIdx%2 == 0 {
						val = b >> 4
					} else {
						val = b & 0x0F
					}
					charGrid[i] = val
					changedIdx++
				}
			}

			colorMaskSize := (int(colorWidth*colorHeight) + 7) / 8
			colorMask := make([]byte, colorMaskSize)
			if _, err := io.ReadFull(buf, colorMask); err != nil {
				return fmt.Errorf("failed to read color mask in P-frame %d: %w", f, err)
			}

			numChangedColors := 0
			for _, b := range colorMask {
				for bit := 0; bit < 8; bit++ {
					if (b & (1 << bit)) != 0 {
						numChangedColors++
					}
				}
			}

			// Read changed colors (packed 4-bit if paletteSize <= 16)
			var changedColors []byte
			if paletteSize <= 16 {
				packedColorSize := (numChangedColors + 1) / 2
				packedColors := make([]byte, packedColorSize)
				if _, err := io.ReadFull(buf, packedColors); err != nil {
					return fmt.Errorf("failed to read packed changed colors in P-frame %d: %w", f, err)
				}
				changedColors = make([]byte, numChangedColors)
				for i := 0; i < numChangedColors; i++ {
					b := packedColors[i/2]
					if i%2 == 0 {
						changedColors[i] = b >> 4
					} else {
						changedColors[i] = b & 0x0F
					}
				}
			} else {
				changedColors = make([]byte, numChangedColors)
				if _, err := io.ReadFull(buf, changedColors); err != nil {
					return fmt.Errorf("failed to read changed colors in P-frame %d: %w", f, err)
				}
			}

			changedColIdx := 0
			for i := 0; i < int(colorWidth*colorHeight); i++ {
				byteIdx := i / 8
				bitIdx := i % 8
				if (colorMask[byteIdx] & (1 << bitIdx)) != 0 {
					colorIndices[i] = changedColors[changedColIdx]
					changedColIdx++
				}
			}
		}

		// Compute edge flags dynamically
		for cy := 0; cy < int(colorHeight); cy++ {
			for cx := 0; cx < int(colorWidth); cx++ {
				xStart := cx * int(width) / int(colorWidth)
				xEnd := (cx + 1) * int(width) / int(colorWidth)
				yStart := cy * int(height) / int(colorHeight)
				yEnd := (cy + 1) * int(height) / int(colorHeight)

				var d00, d10, d01, d11 byte
				d00 = charGrid[yStart*int(width)+xStart]

				xRight := xEnd - 1
				if xRight < xStart {
					xRight = xStart
				}
				yBottom := yEnd - 1
				if yBottom < yStart {
					yBottom = yStart
				}

				d10 = charGrid[yStart*int(width)+xRight]
				d01 = charGrid[yBottom*int(width)+xStart]
				d11 = charGrid[yBottom*int(width)+xRight]

				dh := absDiff(d00, d10) + absDiff(d01, d11)
				dv := absDiff(d00, d01) + absDiff(d10, d11)

				var flag byte = 0
				if dh > 2 || dv > 2 {
					if dh > dv {
						flag = 1
					} else if dv > dh {
						flag = 2
					} else {
						flag = 3
					}
				}
				edgeFlags[cy*int(colorWidth)+cx] = flag
			}
		}

		// Reconstruct cell list for Art
		cells := make([]Cell, width*height)
		paletteLen := len(Palette)

		for y := 0; y < int(height); y++ {
			for x := 0; x < int(width); x++ {
				charIdx := charGrid[y*int(width)+x]
				if int(charIdx) >= paletteLen {
					charIdx = 0
				}
				char := Palette[charIdx]

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

				getNeighCharIdx := func(lx, ly int) byte {
					hx := lx * int(width) / int(colorWidth)
					hy := ly * int(height) / int(colorHeight)
					if hx >= int(width) {
						hx = int(width) - 1
					}
					if hy >= int(height) {
						hy = int(height) - 1
					}
					return charGrid[hy*int(width)+hx]
				}

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

				closestX := x0
				if dx >= 0.5 {
					closestX = x1
				}
				closestY := y0
				if dy >= 0.5 {
					closestY = y1
				}
				edgeFlag := edgeFlags[closestY*int(colorWidth)+closestX]

				if edgeFlag == 1 {
					leftChar := getNeighCharIdx(x0, closestY)
					rightChar := getNeighCharIdx(x1, closestY)
					if absDiff(charIdx, leftChar) < absDiff(charIdx, rightChar) {
						w10 *= 0.05
						w11 *= 0.05
					} else {
						w00 *= 0.05
						w01 *= 0.05
					}
				} else if edgeFlag == 2 {
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

		art := &Art{
			Width:      int(width),
			Height:     int(height),
			OrigWidth:  int(width),
			OrigHeight: int(height),
			Cells:      cells,
		}

		outPath := filepath.Join(outputDir, fmt.Sprintf("frame_%04d.png", f))

		var expErr error
		if mode == "pixel" {
			expErr = ExportPixel(art, outPath)
		} else {
			expErr = ExportRender(art, outPath)
		}

		if expErr != nil {
			return fmt.Errorf("failed to export frame %d: %w", f, expErr)
		}
	}

	return nil
}

// ReadNextImage reads the next complete JPEG or PNG image from the reader.
func ReadNextImage(r *bufio.Reader) (image.Image, error) {
	var buf bytes.Buffer

	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}

		if b == 0x89 {
			header := make([]byte, 7)
			if _, err := io.ReadFull(r, header); err != nil {
				return nil, err
			}
			if bytes.Equal(header, []byte{0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
				buf.WriteByte(0x89)
				buf.Write(header)
				return readPNG(r, &buf)
			}
		} else if b == 0xFF {
			next, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			if next == 0xD8 {
				buf.WriteByte(0xFF)
				buf.WriteByte(0xD8)
				return readJPEG(r, &buf)
			}
		}
	}
}

func readPNG(r *bufio.Reader, buf *bytes.Buffer) (image.Image, error) {
	last4 := make([]byte, 4)
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		buf.WriteByte(b)

		last4[0], last4[1], last4[2], last4[3] = last4[1], last4[2], last4[3], b

		if bytes.Equal(last4, []byte{0x49, 0x45, 0x4E, 0x44}) {
			crc := make([]byte, 4)
			if _, err := io.ReadFull(r, crc); err != nil {
				return nil, err
			}
			buf.Write(crc)
			break
		}
	}

	img, _, err := image.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("failed to decode PNG frame: %w", err)
	}
	return img, nil
}

func readJPEG(r *bufio.Reader, buf *bytes.Buffer) (image.Image, error) {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		buf.WriteByte(b)

		if b == 0xFF {
			m, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			buf.WriteByte(m)

			if m == 0xD9 {
				break
			}
		}
	}

	img, _, err := image.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("failed to decode JPEG frame: %w", err)
	}
	return img, nil
}

// StreamEncodeVideo reads PNG/JPEG images from 'in', compresses them as a GAV stream, and writes to 'out'.
func StreamEncodeVideo(in io.Reader, out io.Writer, targetWidth int, fps int, colorScale int) error {
	r := bufio.NewReader(in)

	fmt.Fprintln(os.Stderr, "Waiting for first frame on stdin...")
	firstImg, err := ReadNextImage(r)
	if err != nil {
		return fmt.Errorf("failed to read first frame: %w", err)
	}
	fmt.Fprintln(os.Stderr, "First frame received! Initializing stream...")

	origW := firstImg.Bounds().Dx()
	origH := firstImg.Bounds().Dy()

	var actualTargetW int
	if targetWidth <= 0 {
		actualTargetW = origW
	} else {
		actualTargetW = targetWidth
	}

	var resizedFirst image.Image
	if targetWidth <= 0 {
		resizedFirst = firstImg
	} else {
		resizedFirst = ResizeBilinear(firstImg, actualTargetW, 0.5)
	}

	width := uint32(resizedFirst.Bounds().Dx())
	height := uint32(resizedFirst.Bounds().Dy())

	if colorScale < 1 {
		colorScale = 1
	}

	colorWidth := width / uint32(colorScale)
	if colorWidth < 1 {
		colorWidth = 1
	}
	colorHeight := height / uint32(colorScale)
	if colorHeight < 1 {
		colorHeight = 1
	}

	firstArt := ConvertToASCII(resizedFirst, origW, origH)
	firstColorGrid := make([]color.RGBA, colorWidth*colorHeight)
	for cy := 0; cy < int(colorHeight); cy++ {
		for cx := 0; cx < int(colorWidth); cx++ {
			xStart := cx * int(width) / int(colorWidth)
			xEnd := (cx + 1) * int(width) / int(colorWidth)
			yStart := cy * int(height) / int(colorHeight)
			yEnd := (cy + 1) * int(height) / int(colorHeight)

			var sumR, sumG, sumB int
			count := 0
			for y := yStart; y < yEnd; y++ {
				for x := xStart; x < xEnd; x++ {
					cell := firstArt.Cells[y*int(width)+x]
					sumR += int(cell.R)
					sumG += int(cell.G)
					sumB += int(cell.B)
					count++
				}
			}
			col := color.RGBA{R: 0, G: 0, B: 0, A: 255}
			if count > 0 {
				col.R = uint8(sumR / count)
				col.G = uint8(sumG / count)
				col.B = uint8(sumB / count)
			}
			firstColorGrid[cy*int(colorWidth)+cx] = col
		}
	}

	globalPalette, _ := QuantizeColors(firstColorGrid)
	paletteSize := len(globalPalette)

	if _, err := out.Write([]byte(GAVMagic)); err != nil {
		return fmt.Errorf("failed to write magic: %w", err)
	}

	headerFields := []uint32{width, height, colorWidth, colorHeight, uint32(fps), 0xFFFFFFFF}
	for _, val := range headerFields {
		if err := binary.Write(out, binary.BigEndian, val); err != nil {
			return fmt.Errorf("failed to write header field: %w", err)
		}
	}

	if err := binary.Write(out, binary.BigEndian, uint32(paletteSize)); err != nil {
		return fmt.Errorf("failed to write palette size: %w", err)
	}

	palBytes := make([]byte, paletteSize*3)
	for i, c := range globalPalette {
		palBytes[i*3] = c.R
		palBytes[i*3+1] = c.G
		palBytes[i*3+2] = c.B
	}
	if _, err := out.Write(palBytes); err != nil {
		return fmt.Errorf("failed to write global palette: %w", err)
	}

	zw, err := zstd.NewWriter(nil)
	if err != nil {
		return fmt.Errorf("failed to create zstd writer: %w", err)
	}
	defer zw.Close()

	var prevCharGrid []byte
	var prevColorIndices []byte

	colorIndices := make([]byte, colorWidth*colorHeight)
	for i, col := range firstColorGrid {
		colorIndices[i] = byte(findClosestColor(col, globalPalette))
	}

	charGrid := make([]byte, width*height)
	for i := 0; i < int(width*height); i++ {
		charGrid[i] = getPaletteIndex(firstArt.Cells[i].Char)
	}

	if err := writeIFrame(out, zw, width, height, charGrid, colorIndices); err != nil {
		return fmt.Errorf("failed to write first I-frame: %w", err)
	}
	prevCharGrid = charGrid
	prevColorIndices = colorIndices

	f := 1
	for {
		img, err := ReadNextImage(r)
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read frame %d: %w", f, err)
		}

		var resized image.Image
		if targetWidth <= 0 {
			resized = img
		} else {
			resized = ResizeBilinear(img, int(width), 0.5)
		}

		art := ConvertToASCII(resized, origW, origH)

		charGrid = make([]byte, width*height)
		for i := 0; i < int(width*height); i++ {
			charGrid[i] = getPaletteIndex(art.Cells[i].Char)
		}

		colorIndices = make([]byte, colorWidth*colorHeight)
		for cy := 0; cy < int(colorHeight); cy++ {
			for cx := 0; cx < int(colorWidth); cx++ {
				xStart := cx * int(width) / int(colorWidth)
				xEnd := (cx + 1) * int(width) / int(colorWidth)
				yStart := cy * int(height) / int(colorHeight)
				yEnd := (cy + 1) * int(height) / int(colorHeight)

				var sumR, sumG, sumB int
				count := 0
				for y := yStart; y < yEnd; y++ {
					for x := xStart; x < xEnd; x++ {
						cell := art.Cells[y*int(width)+x]
						sumR += int(cell.R)
						sumG += int(cell.G)
						sumB += int(cell.B)
						count++
					}
				}
				col := color.RGBA{R: 0, G: 0, B: 0, A: 255}
				if count > 0 {
					col.R = uint8(sumR / count)
					col.G = uint8(sumG / count)
					col.B = uint8(sumB / count)
				}
				colorIndices[cy*int(colorWidth)+cx] = byte(findClosestColor(col, globalPalette))
			}
		}

		isKeyframe := f%60 == 0

		if isKeyframe {
			if err := writeIFrame(out, zw, width, height, charGrid, colorIndices); err != nil {
				return fmt.Errorf("failed to write I-frame %d: %w", f, err)
			}
		} else {
			if err := writePFrame(out, zw, width, height, colorWidth, colorHeight, uint32(paletteSize), charGrid, prevCharGrid, colorIndices, prevColorIndices); err != nil {
				return fmt.Errorf("failed to write P-frame %d: %w", f, err)
			}
		}

		prevCharGrid = charGrid
		prevColorIndices = colorIndices
		f++
	}

	return nil
}

func findClosestColor(c color.RGBA, palette []color.RGBA) int {
	minDist := math.MaxFloat64
	minIndex := 0
	for i, p := range palette {
		dr := float64(c.R) - float64(p.R)
		dg := float64(c.G) - float64(p.G)
		db := float64(c.B) - float64(p.B)
		dist := dr*dr + dg*dg + db*db
		if dist < minDist {
			minDist = dist
			minIndex = i
		}
	}
	return minIndex
}

func writeIFrame(out io.Writer, zw *zstd.Encoder, width, height uint32, charGrid, colorIndices []byte) error {
	var rawBuf bytes.Buffer

	rleChars := EncodeRLE(charGrid)
	if err := binary.Write(&rawBuf, binary.BigEndian, uint32(len(rleChars))); err != nil {
		return err
	}
	rawBuf.Write(rleChars)

	rleColors := EncodeRLE(colorIndices)
	rawBuf.Write(rleColors)

	payload := zw.EncodeAll(rawBuf.Bytes(), nil)

	if err := binary.Write(out, binary.BigEndian, byte(0)); err != nil {
		return err
	}
	if err := binary.Write(out, binary.BigEndian, uint32(len(payload))); err != nil {
		return err
	}
	if _, err := out.Write(payload); err != nil {
		return err
	}

	if f, ok := out.(interface{ Flush() error }); ok {
		f.Flush()
	}

	return nil
}

func writePFrame(out io.Writer, zw *zstd.Encoder, width, height, colorWidth, colorHeight, paletteSize uint32, charGrid, prevCharGrid, colorIndices, prevColorIndices []byte) error {
	var rawBuf bytes.Buffer

	charMask := make([]byte, (int(width*height)+7)/8)
	var changedChars []byte
	for i := 0; i < int(width*height); i++ {
		if charGrid[i] != prevCharGrid[i] {
			byteIdx := i / 8
			bitIdx := i % 8
			charMask[byteIdx] |= (1 << bitIdx)
			changedChars = append(changedChars, charGrid[i])
		}
	}
	rawBuf.Write(charMask)

	packedChanged := make([]byte, (len(changedChars)+1)/2)
	for i := 0; i < len(changedChars); i += 2 {
		val1 := changedChars[i]
		var val2 byte = 0
		if i+1 < len(changedChars) {
			val2 = changedChars[i+1]
		}
		packedChanged[i/2] = (val1 << 4) | (val2 & 0x0F)
	}
	rawBuf.Write(packedChanged)

	colorMask := make([]byte, (int(colorWidth*colorHeight)+7)/8)
	var changedColors []byte
	for i := 0; i < int(colorWidth*colorHeight); i++ {
		if colorIndices[i] != prevColorIndices[i] {
			byteIdx := i / 8
			bitIdx := i % 8
			colorMask[byteIdx] |= (1 << bitIdx)
			changedColors = append(changedColors, colorIndices[i])
		}
	}
	rawBuf.Write(colorMask)

	// Write changed colors (packed 4-bit if paletteSize <= 16)
	if paletteSize <= 16 {
		packed := make([]byte, (len(changedColors)+1)/2)
		for i := 0; i < len(changedColors); i += 2 {
			val1 := changedColors[i]
			var val2 byte = 0
			if i+1 < len(changedColors) {
				val2 = changedColors[i+1]
			}
			packed[i/2] = (val1 << 4) | (val2 & 0x0F)
		}
		rawBuf.Write(packed)
	} else {
		rawBuf.Write(changedColors)
	}

	payload := zw.EncodeAll(rawBuf.Bytes(), nil)

	if err := binary.Write(out, binary.BigEndian, byte(1)); err != nil {
		return err
	}
	if err := binary.Write(out, binary.BigEndian, uint32(len(payload))); err != nil {
		return err
	}
	if _, err := out.Write(payload); err != nil {
		return err
	}

	if f, ok := out.(interface{ Flush() error }); ok {
		f.Flush()
	}

	return nil
}

// StreamDecodeVideo reads a GAV video stream from 'in' and plays it in real-time in the terminal.
func StreamDecodeVideo(in io.Reader) error {
	magic := make([]byte, 4)
	if _, err := io.ReadFull(in, magic); err != nil {
		return fmt.Errorf("failed to read magic: %w", err)
	}
	if string(magic) != GAVMagic {
		return fmt.Errorf("invalid video format: expected %s, got %s", GAVMagic, string(magic))
	}

	var width, height, colorWidth, colorHeight, fps, frameCount uint32
	fields := []*uint32{&width, &height, &colorWidth, &colorHeight, &fps, &frameCount}
	for _, ptr := range fields {
		if err := binary.Read(in, binary.BigEndian, ptr); err != nil {
			return fmt.Errorf("failed to read header field: %w", err)
		}
	}

	var paletteSize uint32
	if err := binary.Read(in, binary.BigEndian, &paletteSize); err != nil {
		return fmt.Errorf("failed to read palette size: %w", err)
	}

	palBytes := make([]byte, paletteSize*3)
	if _, err := io.ReadFull(in, palBytes); err != nil {
		return fmt.Errorf("failed to read global palette: %w", err)
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

	zr, err := zstd.NewReader(nil)
	if err != nil {
		return fmt.Errorf("failed to initialize zstd decoder: %w", err)
	}
	defer zr.Close()

	charGrid := make([]byte, width*height)
	colorIndices := make([]byte, colorWidth*colorHeight)
	edgeFlags := make([]byte, colorWidth*colorHeight)

	fmt.Print("\x1b[?25l\x1b[2J\x1b[H")
	defer fmt.Print("\x1b[?25h\x1b[0m\n")

	frameInterval := time.Second / time.Duration(fps)

	f := 0
	for {
		frameStartTime := time.Now()

		var frameType byte
		var payloadSize uint32
		if err := binary.Read(in, binary.BigEndian, &frameType); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read frame type at frame %d: %w", f, err)
		}
		if err := binary.Read(in, binary.BigEndian, &payloadSize); err != nil {
			return fmt.Errorf("failed to read payload size at frame %d: %w", f, err)
		}

		payload := make([]byte, payloadSize)
		if _, err := io.ReadFull(in, payload); err != nil {
			return fmt.Errorf("failed to read frame payload at frame %d: %w", f, err)
		}

		decompressed, err := zr.DecodeAll(payload, nil)
		if err != nil {
			return fmt.Errorf("failed to decompress frame %d: %w", f, err)
		}

		buf := bytes.NewReader(decompressed)

		if frameType == 0 {
			var rleCharsLen uint32
			if err := binary.Read(buf, binary.BigEndian, &rleCharsLen); err != nil {
				return fmt.Errorf("failed to read rle chars len in I-frame %d: %w", f, err)
			}
			rleChars := make([]byte, rleCharsLen)
			if _, err := io.ReadFull(buf, rleChars); err != nil {
				return fmt.Errorf("failed to read rle chars in I-frame %d: %w", f, err)
			}
			decodedChars, err := DecodeRLE(rleChars)
			if err != nil {
				return fmt.Errorf("failed to decode RLE chars in I-frame %d: %w", f, err)
			}
			copy(charGrid, decodedChars)

			rleColors := make([]byte, buf.Len())
			if _, err := io.ReadFull(buf, rleColors); err != nil {
				return fmt.Errorf("failed to read RLE colors in I-frame %d: %w", f, err)
			}
			decodedColors, err := DecodeRLE(rleColors)
			if err != nil {
				return fmt.Errorf("failed to decode RLE in I-frame %d: %w", f, err)
			}
			copy(colorIndices, decodedColors)

		} else {
			charMaskSize := (int(width*height) + 7) / 8
			charMask := make([]byte, charMaskSize)
			if _, err := io.ReadFull(buf, charMask); err != nil {
				return fmt.Errorf("failed to read char mask in P-frame %d: %w", f, err)
			}

			numChangedChars := 0
			for _, b := range charMask {
				for bit := 0; bit < 8; bit++ {
					if (b & (1 << bit)) != 0 {
						numChangedChars++
					}
				}
			}

			packedChangedCharSize := (numChangedChars + 1) / 2
			packedChanged := make([]byte, packedChangedCharSize)
			if _, err := io.ReadFull(buf, packedChanged); err != nil {
				return fmt.Errorf("failed to read packed changed chars in P-frame %d: %w", f, err)
			}

			changedIdx := 0
			for i := 0; i < int(width*height); i++ {
				byteIdx := i / 8
				bitIdx := i % 8
				if (charMask[byteIdx] & (1 << bitIdx)) != 0 {
					b := packedChanged[changedIdx/2]
					var val byte
					if changedIdx%2 == 0 {
						val = b >> 4
					} else {
						val = b & 0x0F
					}
					charGrid[i] = val
					changedIdx++
				}
			}

			colorMaskSize := (int(colorWidth*colorHeight) + 7) / 8
			colorMask := make([]byte, colorMaskSize)
			if _, err := io.ReadFull(buf, colorMask); err != nil {
				return fmt.Errorf("failed to read color mask in P-frame %d: %w", f, err)
			}

			numChangedColors := 0
			for _, b := range colorMask {
				for bit := 0; bit < 8; bit++ {
					if (b & (1 << bit)) != 0 {
						numChangedColors++
					}
				}
			}

			// Read changed colors (packed 4-bit if paletteSize <= 16)
			var changedColors []byte
			if paletteSize <= 16 {
				packedColorSize := (numChangedColors + 1) / 2
				packedColors := make([]byte, packedColorSize)
				if _, err := io.ReadFull(buf, packedColors); err != nil {
					return fmt.Errorf("failed to read packed changed colors in P-frame %d: %w", f, err)
				}
				changedColors = make([]byte, numChangedColors)
				for i := 0; i < numChangedColors; i++ {
					b := packedColors[i/2]
					if i%2 == 0 {
						changedColors[i] = b >> 4
					} else {
						changedColors[i] = b & 0x0F
					}
				}
			} else {
				changedColors = make([]byte, numChangedColors)
				if _, err := io.ReadFull(buf, changedColors); err != nil {
					return fmt.Errorf("failed to read changed colors in P-frame %d: %w", f, err)
				}
			}

			changedColIdx := 0
			for i := 0; i < int(colorWidth*colorHeight); i++ {
				byteIdx := i / 8
				bitIdx := i % 8
				if (colorMask[byteIdx] & (1 << bitIdx)) != 0 {
					colorIndices[i] = changedColors[changedColIdx]
					changedColIdx++
				}
			}
		}

		for cy := 0; cy < int(colorHeight); cy++ {
			for cx := 0; cx < int(colorWidth); cx++ {
				xStart := cx * int(width) / int(colorWidth)
				xEnd := (cx + 1) * int(width) / int(colorWidth)
				yStart := cy * int(height) / int(colorHeight)
				yEnd := (cy + 1) * int(height) / int(colorHeight)

				var d00, d10, d01, d11 byte
				d00 = charGrid[yStart*int(width)+xStart]

				xRight := xEnd - 1
				if xRight < xStart {
					xRight = xStart
				}
				yBottom := yEnd - 1
				if yBottom < yStart {
					yBottom = yStart
				}

				d10 = charGrid[yStart*int(width)+xRight]
				d01 = charGrid[yBottom*int(width)+xStart]
				d11 = charGrid[yBottom*int(width)+xRight]

				dh := absDiff(d00, d10) + absDiff(d01, d11)
				dv := absDiff(d00, d01) + absDiff(d10, d11)

				var flag byte = 0
				if dh > 2 || dv > 2 {
					if dh > dv {
						flag = 1
					} else if dv > dh {
						flag = 2
					} else {
						flag = 3
					}
				}
				edgeFlags[cy*int(colorWidth)+cx] = flag
			}
		}

		fmt.Print("\x1b[H")

		paletteLen := len(Palette)
		var curR, curG, curB uint8
		firstColor := true

		var lineBuf strings.Builder
		lineBuf.Grow(int(width)*25 + int(height))

		for y := 0; y < int(height); y++ {
			for x := 0; x < int(width); x++ {
				charIdx := charGrid[y*int(width)+x]
				if int(charIdx) >= paletteLen {
					charIdx = 0
				}
				char := Palette[charIdx]

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

				getNeighCharIdx := func(lx, ly int) byte {
					hx := lx * int(width) / int(colorWidth)
					hy := ly * int(height) / int(colorHeight)
					if hx >= int(width) {
						hx = int(width) - 1
					}
					if hy >= int(height) {
						hy = int(height) - 1
					}
					return charGrid[hy*int(width)+hx]
				}

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

				closestX := x0
				if dx >= 0.5 {
					closestX = x1
				}
				closestY := y0
				if dy >= 0.5 {
					closestY = y1
				}
				edgeFlag := edgeFlags[closestY*int(colorWidth)+closestX]

				if edgeFlag == 1 {
					leftChar := getNeighCharIdx(x0, closestY)
					rightChar := getNeighCharIdx(x1, closestY)
					if absDiff(charIdx, leftChar) < absDiff(charIdx, rightChar) {
						w10 *= 0.05
						w11 *= 0.05
					} else {
						w00 *= 0.05
						w01 *= 0.05
					}
				} else if edgeFlag == 2 {
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
					r = uint8(math.Round((1.0-dx)*(1.0-dy)*float64(c00.R) + dx*(1.0-dy)*float64(c10.R) + (1.0-dx)*dy*float64(c01.R) + dx*dy*float64(c11.R)))
					g = uint8(math.Round((1.0-dx)*(1.0-dy)*float64(c00.G) + dx*(1.0-dy)*float64(c10.G) + (1.0-dx)*dy*float64(c01.G) + dx*dy*float64(c11.G)))
					b = uint8(math.Round((1.0-dx)*(1.0-dy)*float64(c00.B) + dx*(1.0-dy)*float64(c10.B) + (1.0-dx)*dy*float64(c11.B)))
				}

				if firstColor || r != curR || g != curG || b != curB {
					lineBuf.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b))
					curR, curG, curB = r, g, b
					firstColor = false
				}
				lineBuf.WriteRune(char)
			}
			lineBuf.WriteString("\x1b[0m\n")
			firstColor = true
		}
		fmt.Print(lineBuf.String())

		elapsed := time.Since(frameStartTime)
		if elapsed < frameInterval {
			time.Sleep(frameInterval - elapsed)
		}
		f++
	}

	return nil
}
