package provider

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"strings"

	"golang.org/x/image/webp"
)

// DetectImageFormat returns png, jpeg, or webp from image magic bytes.
func DetectImageFormat(data []byte) (string, error) {
	dims, err := ReadImageDimensions(data)
	if err != nil {
		return "", err
	}
	switch dims.MIMEType {
	case "image/png":
		return "png", nil
	case "image/jpeg":
		return "jpeg", nil
	case "image/webp":
		return "webp", nil
	default:
		return "", fmt.Errorf("unsupported image format")
	}
}

// EncodeImageFormat re-encodes image bytes into the requested format.
// compression is used for jpeg (0-100). webp can only be preserved when the
// source is already webp; there is no pure-Go webp encoder in this module.
func EncodeImageFormat(data []byte, format string, compression *int) ([]byte, error) {
	format = strings.TrimSpace(strings.ToLower(format))
	if format == "webp" {
		detected, err := DetectImageFormat(data)
		if err != nil {
			return nil, err
		}
		if detected != "webp" {
			return nil, fmt.Errorf("webp output requires webp source bytes")
		}
		return data, nil
	}
	img, err := decodeImageBytes(data)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	switch format {
	case "png":
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
	case "jpeg":
		quality := 90
		if compression != nil {
			quality = *compression
		}
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported output format %q", format)
	}
	return buf.Bytes(), nil
}

func decodeImageBytes(data []byte) (image.Image, error) {
	if isWebP(data) {
		img, err := webp.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode webp: %w", err)
		}
		return img, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return img, nil
}

// ApplyGoogleOutputFormat applies AS-local Google output_format rules.
func ApplyGoogleOutputFormat(data []byte, outputFormat string, compression *int) (encoded []byte, format string, err error) {
	if strings.TrimSpace(outputFormat) == "" {
		format, err = DetectImageFormat(data)
		if err != nil {
			return nil, "", err
		}
		return data, format, nil
	}
	encoded, err = EncodeImageFormat(data, outputFormat, compression)
	if err != nil {
		return nil, "", err
	}
	return encoded, outputFormat, nil
}
