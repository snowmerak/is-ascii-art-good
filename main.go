package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"

	"is-ascii-art-good/ascii"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "compress":
		if len(os.Args) < 4 {
			fmt.Println("Error: missing arguments for 'compress'")
			printUsage()
			os.Exit(1)
		}
		inputPath := os.Args[2]
		outputPath := os.Args[3]

		width := 100
		if len(os.Args) >= 5 {
			w, err := strconv.Atoi(os.Args[4])
			if err != nil || w <= 0 {
				fmt.Printf("Invalid width '%s', using default 100\n", os.Args[4])
			} else {
				width = w
			}
		}

		if err := runCompress(inputPath, outputPath, width); err != nil {
			fmt.Fprintf(os.Stderr, "Compression failed: %v\n", err)
			os.Exit(1)
		}

	case "view":
		if len(os.Args) < 3 {
			fmt.Println("Error: missing argument for 'view'")
			printUsage()
			os.Exit(1)
		}
		inputPath := os.Args[2]

		if err := runView(inputPath); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to view: %v\n", err)
			os.Exit(1)
		}

	case "export":
		if len(os.Args) < 4 {
			fmt.Println("Error: missing arguments for 'export'")
			printUsage()
			os.Exit(1)
		}
		inputPath := os.Args[2]
		outputPath := os.Args[3]

		mode := "pixel"
		if len(os.Args) >= 5 {
			mode = os.Args[4]
		}

		if err := runExport(inputPath, outputPath, mode); err != nil {
			fmt.Fprintf(os.Stderr, "Export failed: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Printf("Error: unknown command '%s'\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  go run main.go compress <input_image_path> <output_gac_path> [target_width]")
	fmt.Println("  go run main.go view <input_gac_path>")
	fmt.Println("  go run main.go export <input_gac_path> <output_image_path> [pixel|render]")
}

func runCompress(inputPath, outputPath string, width int) error {
	// 1. Get original file size
	inputInfo, err := os.Stat(inputPath)
	if err != nil {
		return fmt.Errorf("failed to check input file: %w", err)
	}
	origSize := inputInfo.Size()

	// 2. Load image
	fmt.Printf("Loading %s...\n", filepath.Base(inputPath))
	img, err := ascii.LoadImage(inputPath)
	if err != nil {
		return err
	}

	// 3. Resize image
	fmt.Printf("Resizing to width %d (adjusting aspect ratio)...\n", width)
	resizedImg := ascii.ResizeBilinear(img, width, 0.5)

	// 4. Convert to ASCII art representation
	fmt.Println("Converting to ASCII and extracting colors...")
	art := ascii.ConvertToASCII(resizedImg, img.Bounds().Dx(), img.Bounds().Dy())

	// 5. Save compressed file
	fmt.Printf("Compressing and saving to %s...\n", outputPath)
	if err := ascii.SaveGAC(art, outputPath); err != nil {
		return err
	}

	// 6. Get compressed file size
	outputInfo, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("failed to check output file: %w", err)
	}
	compSize := outputInfo.Size()

	ratio := float64(origSize) / float64(compSize)
	percentSaved := (1.0 - float64(compSize)/float64(origSize)) * 100.0

	fmt.Println("\nCompression success!")
	fmt.Printf("Original image size:   %10d bytes\n", origSize)
	fmt.Printf("Compressed .gac size:  %10d bytes\n", compSize)
	fmt.Printf("Compression ratio:     %.2fx (Saved %.2f%%)\n", ratio, percentSaved)
	fmt.Printf("Dimensions:            %dx%d characters\n", art.Width, art.Height)

	return nil
}

func runView(inputPath string) error {
	art, err := ascii.LoadGAC(inputPath)
	if err != nil {
		return err
	}

	var curR, curG, curB uint8
	first := true

	for y := 0; y < art.Height; y++ {
		for x := 0; x < art.Width; x++ {
			cell := art.Cells[y*art.Width+x]
			if first || cell.R != curR || cell.G != curG || cell.B != curB {
				fmt.Printf("\x1b[38;2;%d;%d;%dm", cell.R, cell.G, cell.B)
				curR, curG, curB = cell.R, cell.G, cell.B
				first = false
			}
			fmt.Print(string(cell.Char))
		}
		fmt.Println("\x1b[0m")
		first = true
	}

	return nil
}

func runExport(inputPath, outputPath, mode string) error {
	fmt.Printf("Loading compressed file %s...\n", filepath.Base(inputPath))
	art, err := ascii.LoadGAC(inputPath)
	if err != nil {
		return err
	}

	switch mode {
	case "pixel":
		targetHeight := art.Height
		if art.OrigWidth > 0 && art.OrigHeight > 0 {
			targetHeight = int(math.Round(float64(art.Width) * float64(art.OrigHeight) / float64(art.OrigWidth)))
			if targetHeight < 1 {
				targetHeight = 1
			}
		}
		fmt.Printf("Reconstructing pixel image (%dx%d) and saving to %s...\n", art.Width, targetHeight, outputPath)
		return ascii.ExportPixel(art, outputPath)
	case "render":
		targetHeight := art.Height * 8
		if art.OrigWidth > 0 && art.OrigHeight > 0 {
			targetHeight = int(math.Round(float64(art.Width*8) * float64(art.OrigHeight) / float64(art.OrigWidth)))
			if targetHeight < 8 {
				targetHeight = 8
			}
		}
		fmt.Printf("Rendering ASCII glyph image (%dx%d) and saving to %s...\n", art.Width*8, targetHeight, outputPath)
		return ascii.ExportRender(art, outputPath)
	default:
		return fmt.Errorf("unknown export mode '%s'; use 'pixel' or 'render'", mode)
	}
}
