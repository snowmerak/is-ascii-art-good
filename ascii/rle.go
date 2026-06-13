package ascii

import "fmt"

// EncodeRLE compresses the byte slice using Run-Length Encoding.
// Each run is represented as [Count, Value] where Count fits in a single byte (1 to 255).
func EncodeRLE(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}

	var rle []byte
	n := len(data)
	i := 0

	for i < n {
		val := data[i]
		count := 1

		for i+count < n && data[i+count] == val && count < 255 {
			count++
		}

		rle = append(rle, byte(count), val)
		i += count
	}

	return rle
}

// DecodeRLE expands the RLE-compressed byte slice back to its original form.
func DecodeRLE(rleData []byte) ([]byte, error) {
	if len(rleData)%2 != 0 {
		return nil, fmt.Errorf("invalid RLE data: odd number of bytes (%d)", len(rleData))
	}

	var data []byte
	for i := 0; i < len(rleData); i += 2 {
		count := int(rleData[i])
		val := rleData[i+1]

		for j := 0; j < count; j++ {
			data = append(data, val)
		}
	}

	return data, nil
}
