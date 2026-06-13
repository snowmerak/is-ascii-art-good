# is-ascii-art-good

A Go-based ultra-efficient ASCII image (`.gac`) and video (`.gav`) compressor, terminal player, and exporter.

By combining hybrid resolution (high-resolution character luminance + low/full-resolution quantized color channels) and temporal delta-frame encoding, it drastically reduces file size while maintaining terminal readability.

## Features

- **Hybrid Resolution Compression (`.gac` Format)**:
  - 4-bit high-resolution character density representation.
  - Configurable color downscaling (1x and 2x) with 256-color palette quantization.
  - **Edge-Guided Bilateral Color Upscaling** which dynamically computes edge flags (Flat, Vertical, Horizontal, Diagonal) from the character density grid to prevent color bleeding at boundaries.
  - Final compressed payload packed using **Zstd** for high-efficiency storage.
- **Delta-Compressed Video (`.gav` Format)**:
  - Global color palette generation across frames to eliminate color flickering.
  - **1-bit Change Mask P-Frame (Delta Frame)** technology to avoid redundant data transmission for static regions.
  - Achieves **>99.9% compression ratio** (against raw pixel bytes) for typical animations.
- **Real-Time Video Streaming (Stdin/Stdout Pipelines)**:
  - Live PNG/JPEG stream parser that splits frames from `stdin`.
  - Zero-latency encoder that flushes `stdout` after each frame.
  - Periodic GOP keyframes (every 60 frames) to allow dynamic decoder attach.
- **Terminal Video Player**:
  - Direct smooth rendering using ANSI Truecolor and cursor control (`\x1b[H` overrides).
- **Multiple Exporter Modes**:
  - **Pixel Mode**: Reconstructs back to standard raster pixel PNG images.
  - **Render Mode**: Renders the actual colored terminal glyph shapes into high-resolution PNG images.

---

## Installation & Build

Requires Go 1.16+ installed.

```bash
# Install dependencies
go mod tidy

# Build executable
go build -o ascii_art.exe
```

---

## Command Line Usage

### 1. Image Compression & Viewing

#### Compress Image (`compress-image`)
```bash
go run . compress-image <input_image_path> <output_gac_path> [target_width|orig] [char_aspect_ratio] [color_scale]
```
- `target_width|orig`: Target character width (e.g. `100`, or `orig` to preserve the original 1:1 image resolution; defaults to `100`).
- `char_aspect_ratio`: Font vertical stretch ratio (defaults to `0.5`).
- `color_scale`: Downscale factor for the color grid (defaults to `1` for perfect 1x pixel color matching, or `2` for 2x downscaled colors to maximize compression).

#### Terminal Viewer (`view-image`)
```bash
go run . view-image <input_gac_path>
```

#### Reconstruct to PNG (`export-image`)
```bash
go run . export-image <input_gac_path> <output_image_path> [pixel|render]
```
- `pixel`: Reconstructs the image back into standard pixels.
- `render`: Renders the output glyphs as they would appear in a terminal into a PNG image.

---

### 2. Video Compression & Playback

#### Generate Test Frames
```bash
go run . generate-test-frames <output_dir> <frame_count>
```

#### Compress Frames into Video (`compress-video`)
```bash
go run . compress-video <frames_dir> <output_gav_path> <fps> [target_width|orig] [color_scale]
```

#### Play Video in Terminal (`play-video`)
```bash
go run . play-video <input_gav_path>
```

#### Export Video to PNG Sequence (`export-video`)
```bash
go run . export-video <input_gav_path> <output_dir> [pixel|render]
```

---

### 3. Real-Time Video Streaming (Stdin/Stdout)

#### Live Video Encoding (`stream-encode-video`)
Reads raw JPEG/PNG image stream from standard input, compresses them, and writes the GAV stream to standard output.
```bash
go run . stream-encode-video [target_width|orig] [fps] [color_scale] < input_pipe
```

#### Live Video Decoding (`stream-decode-video`)
Reads a GAV stream from standard input and plays it in real-time in the terminal.
```bash
go run . stream-decode-video < input_gac_stream
```

#### Stream Test Pipe (`stream-test-pipe`)
Reads all files in a directory and concatenates their binary bytes to standard output. Perfect for mock streaming verification.
```bash
go run . stream-test-pipe <directory_path>
```

#### Complete Live Streaming Pipeline Example:
```bash
# Generate mock frames
go run . generate-test-frames data/test_frames 60

# Stream encode and decode in real-time
go run . stream-test-pipe data/test_frames | go run . stream-encode-video 100 30 1 | go run . stream-decode-video
```

---

## Taskfile Tasks

This project provides a `Taskfile.yml` for testing convenience. If you have `task` installed:

- Run all test suites: `task test` (Runs image, video, and live streaming pipelines)
- Run image test pipeline: `task test-image`
- Run video test pipeline: `task test-video`
- Run live streaming test pipeline: `task test-stream`
- Clean all build and test artifacts: `task clean`
