//go:build js && wasm

package main

import (
	"bytes"
	"syscall/js"

	"is-ascii-art-good/ascii"
)

func main() {
	js.Global().Set("loadGAC", js.FuncOf(jsLoadGAC))
	select {}
}

func jsLoadGAC(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return nil
	}

	jsArray := args[0]
	length := jsArray.Length()
	fileBytes := make([]byte, length)
	js.CopyBytesToGo(fileBytes, jsArray)

	art, err := ascii.DecodeGAC(bytes.NewReader(fileBytes))
	if err != nil {
		return map[string]interface{}{
			"error": err.Error(),
		}
	}

	charIndices := make([]byte, art.Width*art.Height)
	colors := make([]byte, art.Width*art.Height*3)

	for i := 0; i < art.Width*art.Height; i++ {
		cell := art.Cells[i]

		charIdx := byte(0)
		for idx, r := range ascii.Palette {
			if r == cell.Char {
				charIdx = byte(idx)
				break
			}
		}
		charIndices[i] = charIdx

		colors[i*3] = cell.R
		colors[i*3+1] = cell.G
		colors[i*3+2] = cell.B
	}

	jsCharIndices := js.Global().Get("Uint8Array").New(art.Width * art.Height)
	js.CopyBytesToJS(jsCharIndices, charIndices)

	jsColors := js.Global().Get("Uint8Array").New(art.Width * art.Height * 3)
	js.CopyBytesToJS(jsColors, colors)

	return map[string]interface{}{
		"width":       art.Width,
		"height":      art.Height,
		"origWidth":   art.OrigWidth,
		"origHeight":  art.OrigHeight,
		"charIndices": jsCharIndices,
		"colors":      jsColors,
	}
}
