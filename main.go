package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
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
			arg := os.Args[4]
			if arg == "orig" {
				width = 0
			} else {
				w, err := strconv.Atoi(arg)
				if err != nil || w <= 0 {
					fmt.Printf("Invalid width '%s', using default 100\n", arg)
				} else {
					width = w
				}
			}
		}

		aspectRatio := -1.0
		if len(os.Args) >= 6 {
			ar, err := strconv.ParseFloat(os.Args[5], 64)
			if err != nil || ar <= 0.0 {
				fmt.Printf("Invalid aspect ratio '%s', using auto mode\n", os.Args[5])
			} else {
				aspectRatio = ar
			}
		}

		colorScale := 1
		if len(os.Args) >= 7 {
			cs, err := strconv.Atoi(os.Args[6])
			if err == nil && cs > 0 {
				colorScale = cs
			}
		}

		if err := runCompress(inputPath, outputPath, width, aspectRatio, colorScale); err != nil {
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

	case "compress-video":
		if len(os.Args) < 5 {
			fmt.Println("Error: missing arguments for 'compress-video'")
			printVideoUsage()
			os.Exit(1)
		}
		framesDir := os.Args[2]
		outputPath := os.Args[3]
		fps, err := strconv.Atoi(os.Args[4])
		if err != nil || fps <= 0 {
			fmt.Printf("Invalid FPS '%s', using 30\n", os.Args[4])
			fps = 30
		}

		width := 100
		if len(os.Args) >= 6 {
			arg := os.Args[5]
			if arg == "orig" {
				width = 0
			} else {
				w, err := strconv.Atoi(arg)
				if err == nil && w > 0 {
					width = w
				}
			}
		}

		colorScale := 1
		if len(os.Args) >= 7 {
			cs, err := strconv.Atoi(os.Args[6])
			if err == nil && cs > 0 {
				colorScale = cs
			}
		}

		if err := ascii.CompressVideo(framesDir, outputPath, width, fps, colorScale); err != nil {
			fmt.Fprintf(os.Stderr, "Video compression failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Video compression complete!")

	case "play-video":
		if len(os.Args) < 3 {
			fmt.Println("Error: missing argument for 'play-video'")
			printVideoUsage()
			os.Exit(1)
		}
		inputPath := os.Args[2]
		if err := ascii.PlayVideo(inputPath); err != nil {
			fmt.Fprintf(os.Stderr, "Playback failed: %v\n", err)
			os.Exit(1)
		}

	case "export-video":
		if len(os.Args) < 4 {
			fmt.Println("Error: missing arguments for 'export-video'")
			printVideoUsage()
			os.Exit(1)
		}
		inputPath := os.Args[2]
		outputDir := os.Args[3]
		mode := "pixel"
		if len(os.Args) >= 5 {
			mode = os.Args[4]
		}
		if err := ascii.ExportVideo(inputPath, outputDir, mode); err != nil {
			fmt.Fprintf(os.Stderr, "Video export failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Video export complete!")

	case "generate-test-frames":
		if len(os.Args) < 4 {
			fmt.Println("Error: missing arguments for 'generate-test-frames'")
			printVideoUsage()
			os.Exit(1)
		}
		outputDir := os.Args[2]
		count, err := strconv.Atoi(os.Args[3])
		if err != nil || count <= 0 {
			count = 60
		}
		if err := generateTestFrames(outputDir, count); err != nil {
			fmt.Fprintf(os.Stderr, "Test frame generation failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Generated %d test frames in %s\n", count, outputDir)

	default:
		fmt.Printf("Error: unknown command '%s'\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  go run main.go compress <input_image_path> <output_gac_path> [target_width|orig] [char_aspect_ratio] [color_scale]")
	fmt.Println("  go run main.go view <input_gac_path>")
	fmt.Println("  go run main.go export <input_gac_path> <output_image_path> [pixel|render]")
}

func printVideoUsage() {
	printUsage()
	fmt.Println("\nVideo Extensions:")
	fmt.Println("  go run main.go generate-test-frames <output_dir> <frame_count>")
	fmt.Println("  go run main.go compress-video <frames_dir> <output_gav_path> <fps> [target_width|orig] [color_scale]")
	fmt.Println("  go run main.go play-video <input_gav_path>")
	fmt.Println("  go run main.go export-video <input_gav_path> <output_dir> [pixel|render]")
}

func runCompress(inputPath, outputPath string, width int, aspectRatio float64, colorScale int) error {
	inputInfo, err := os.Stat(inputPath)
	if err != nil {
		return fmt.Errorf("failed to check input file: %w", err)
	}
	origSize := inputInfo.Size()

	fmt.Printf("Loading %s...\n", filepath.Base(inputPath))
	img, err := ascii.LoadImage(inputPath)
	if err != nil {
		return err
	}

	origW := img.Bounds().Dx()
	origH := img.Bounds().Dy()

	targetWidth := width
	if targetWidth <= 0 {
		targetWidth = origW
	}

	targetAspectRatio := aspectRatio
	if targetAspectRatio <= 0.0 {
		if width <= 0 {
			targetAspectRatio = 1.0
		} else {
			targetAspectRatio = 0.5
		}
	}

	var resizedImg image.Image
	if targetWidth == origW && targetAspectRatio == 1.0 {
		fmt.Println("Keeping original 1:1 resolution (skipping resizing)...")
		resizedImg = img
	} else {
		fmt.Printf("Resizing to width %d (aspect ratio: %.2f)...\n", targetWidth, targetAspectRatio)
		resizedImg = ascii.ResizeBilinear(img, targetWidth, targetAspectRatio)
	}

	fmt.Println("Converting to ASCII and extracting colors...")
	art := ascii.ConvertToASCII(resizedImg, origW, origH)

	fmt.Printf("Compressing and saving to %s...\n", outputPath)
	if err := ascii.SaveGAC(art, outputPath, colorScale); err != nil {
		return err
	}

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

func generateTestFrames(outputDir string, count int) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}
	width, height := 320, 240
	radius := 30.0

	for i := 0; i < count; i++ {
		t := float64(i) * 0.1
		x := float64(width)/2.0 + float64(width)/3.0*math.Cos(t)
		y := float64(height)/2.0 + float64(height)/3.0*math.Abs(math.Sin(t*1.5)) - 20.0

		img := image.NewRGBA(image.Rect(0, 0, width, height))

		for py := 0; py < height; py++ {
			for px := 0; px < width; px++ {
				img.SetRGBA(px, py, color.RGBA{
					R: uint8(10),
					G: uint8(20 + py*30/height),
					B: uint8(60 + px*40/width),
					A: 255,
				})
			}
		}

		for py := 0; py < height; py++ {
			for px := 0; px < width; px++ {
				dx := float64(px) - x
				dy := float64(py) - y
				dist := math.Sqrt(dx*dx + dy*dy)
				if dist < radius {
					alpha := 1.0
					if dist > radius-2.0 {
						alpha = (radius - dist) / 2.0
					}
					r := uint8(255 * alpha)
					g := uint8(200 * alpha)
					b := uint8(50 * alpha)

					bg := img.RGBAAt(px, py)
					img.SetRGBA(px, py, color.RGBA{
						R: uint8(float64(r)*alpha + float64(bg.R)*(1.0-alpha)),
						G: uint8(float64(g)*alpha + float64(bg.G)*(1.0-alpha)),
						B: uint8(float64(b)*alpha + float64(bg.B)*(1.0-alpha)),
						A: 255,
					})
				}
			}
		}

		outPath := filepath.Join(outputDir, fmt.Sprintf("frame_%04d.png", i))
		f, err := os.Create(outPath)
		if err != nil {
			return err
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return nil
}
