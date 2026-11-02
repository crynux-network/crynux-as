package provider

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
)

// ImageDimensions is the pixel size and detected MIME type of one image.
type ImageDimensions struct {
	MIMEType string
	Width    int
	Height   int
}

var errUnsupportedImageFormat = errors.New("image data is not a valid JPEG, PNG, or WebP image")

// ReadImageDimensions reads only the JPEG, PNG, or WebP header of decoded image
// bytes and returns the detected MIME type and pixel size.
func ReadImageDimensions(data []byte) (ImageDimensions, error) {
	if isWebP(data) {
		return readWebPDimensions(data)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ImageDimensions{}, errUnsupportedImageFormat
	}
	var mimeType string
	switch format {
	case "jpeg":
		mimeType = "image/jpeg"
	case "png":
		mimeType = "image/png"
	default:
		return ImageDimensions{}, errUnsupportedImageFormat
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return ImageDimensions{}, errUnsupportedImageFormat
	}
	return ImageDimensions{MIMEType: mimeType, Width: cfg.Width, Height: cfg.Height}, nil
}

func isWebP(data []byte) bool {
	return len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}

// readWebPDimensions parses the first chunk of a WebP RIFF container:
// VP8 (lossy), VP8L (lossless), or VP8X (extended canvas).
func readWebPDimensions(data []byte) (ImageDimensions, error) {
	if len(data) < 30 {
		return ImageDimensions{}, errUnsupportedImageFormat
	}
	var width, height int
	switch string(data[12:16]) {
	case "VP8 ":
		if data[23] != 0x9d || data[24] != 0x01 || data[25] != 0x2a {
			return ImageDimensions{}, errUnsupportedImageFormat
		}
		width = int(binary.LittleEndian.Uint16(data[26:28]) & 0x3fff)
		height = int(binary.LittleEndian.Uint16(data[28:30]) & 0x3fff)
	case "VP8L":
		if data[20] != 0x2f {
			return ImageDimensions{}, errUnsupportedImageFormat
		}
		bits := binary.LittleEndian.Uint32(data[21:25])
		width = int(bits&0x3fff) + 1
		height = int((bits>>14)&0x3fff) + 1
	case "VP8X":
		width = int(uint32(data[24])|uint32(data[25])<<8|uint32(data[26])<<16) + 1
		height = int(uint32(data[27])|uint32(data[28])<<8|uint32(data[29])<<16) + 1
	default:
		return ImageDimensions{}, errUnsupportedImageFormat
	}
	if width <= 0 || height <= 0 {
		return ImageDimensions{}, errUnsupportedImageFormat
	}
	return ImageDimensions{MIMEType: "image/webp", Width: width, Height: height}, nil
}
