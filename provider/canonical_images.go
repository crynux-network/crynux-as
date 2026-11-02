package provider

import (
	"crynux_as/llmadapter"
	"fmt"
)

var allowedImageMIMETypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
}

var allowedImageDetails = map[string]struct{}{
	"":         {},
	"auto":     {},
	"low":      {},
	"high":     {},
	"original": {},
}

// CanonicalImageInfo is the validated metadata of one canonical image part.
type CanonicalImageInfo struct {
	MIMEType     string
	Width        int
	Height       int
	Detail       string
	DecodedBytes int
}

// ValidateCanonicalImages validates every image part of a canonical request and
// returns image metadata in request order. Images MUST appear only in user
// messages, declare image/jpeg, image/png, or image/webp, carry valid base64
// whose decoded header matches the declared MIME type, and use a supported
// OpenAI detail value. The decoded payload sizes of all images MUST NOT exceed
// maxBytes in total.
func ValidateCanonicalImages(req CanonicalRequest, maxBytes uint64) ([]CanonicalImageInfo, error) {
	var images []CanonicalImageInfo
	var total uint64
	for msgIndex, msg := range req.Messages {
		for _, part := range msg.Parts {
			if part.Type != CanonicalPartImage {
				continue
			}
			imageIndex := len(images)
			if msg.Role != "user" {
				return nil, fmt.Errorf("messages[%d]: images are only supported in user messages", msgIndex)
			}
			if _, ok := allowedImageMIMETypes[part.MIMEType]; !ok {
				return nil, fmt.Errorf("image %d: unsupported MIME type %q; allowed types are image/jpeg, image/png, and image/webp", imageIndex, part.MIMEType)
			}
			if _, ok := allowedImageDetails[part.Detail]; !ok {
				return nil, fmt.Errorf("image %d: unsupported detail %q; allowed values are auto, low, high, and original", imageIndex, part.Detail)
			}
			decoded, err := llmadapter.DecodeBase64Payload(part.Data)
			if err != nil {
				return nil, fmt.Errorf("image %d: %w", imageIndex, err)
			}
			total += uint64(len(decoded))
			if total > maxBytes {
				return nil, fmt.Errorf("total image size exceeds the %d-byte limit", maxBytes)
			}
			dims, err := ReadImageDimensions(decoded)
			if err != nil {
				return nil, fmt.Errorf("image %d: %w", imageIndex, err)
			}
			if dims.MIMEType != part.MIMEType {
				return nil, fmt.Errorf("image %d: declared MIME type %q does not match image data %q", imageIndex, part.MIMEType, dims.MIMEType)
			}
			images = append(images, CanonicalImageInfo{
				MIMEType:     part.MIMEType,
				Width:        dims.Width,
				Height:       dims.Height,
				Detail:       part.Detail,
				DecodedBytes: len(decoded),
			})
		}
	}
	return images, nil
}
