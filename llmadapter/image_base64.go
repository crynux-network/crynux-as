package llmadapter

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

// ParsedDataURL is a validated base64 image data URL.
type ParsedDataURL struct {
	// MIMEType is the lowercase media type declared in the data URL. It may be empty.
	MIMEType string
	// Data is the raw base64 payload without the data URL prefix.
	Data string
}

// ParseImageDataURL validates a data:<mime>;base64,<payload> URL and returns its
// declared MIME type and base64 payload. Remote URLs are rejected.
func ParseImageDataURL(rawURL string) (ParsedDataURL, error) {
	if rawURL == "" {
		return ParsedDataURL{}, fmt.Errorf("image_url.url is required")
	}
	lower := strings.ToLower(rawURL)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return ParsedDataURL{}, fmt.Errorf("image_url.url must be a base64 data URL; HTTP and HTTPS URLs are not supported")
	}

	commaIdx := strings.Index(rawURL, ",")
	if commaIdx <= 0 || commaIdx == len(rawURL)-1 {
		return ParsedDataURL{}, fmt.Errorf("image_url.url must be a non-empty data URL with base64 payload")
	}

	metadata := strings.ToLower(rawURL[:commaIdx])
	payload := rawURL[commaIdx+1:]
	if !strings.HasPrefix(metadata, "data:") || !strings.Contains(metadata, ";base64") {
		return ParsedDataURL{}, fmt.Errorf("image_url.url must use data:*;base64,<payload> format")
	}

	if _, err := DecodeBase64Payload(payload); err != nil {
		return ParsedDataURL{}, fmt.Errorf("image_url.url contains invalid base64 payload")
	}

	mimeType := strings.TrimPrefix(metadata, "data:")
	if idx := strings.Index(mimeType, ";"); idx >= 0 {
		mimeType = mimeType[:idx]
	}
	return ParsedDataURL{MIMEType: strings.TrimSpace(mimeType), Data: payload}, nil
}

// ExtractBase64PayloadFromDataURL extracts and validates the base64 payload from a
// data:*;base64,<payload> URL. It returns the raw base64 payload without the data URL prefix.
func ExtractBase64PayloadFromDataURL(rawURL string) (string, error) {
	parsed, err := ParseImageDataURL(rawURL)
	if err != nil {
		return "", err
	}
	return parsed.Data, nil
}

// FormatImageDataURL builds a data:<mime>;base64,<data> URL.
func FormatImageDataURL(mimeType, data string) string {
	return "data:" + mimeType + ";base64," + data
}

// DecodeBase64Payload decodes standard base64 with or without padding.
func DecodeBase64Payload(data string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err == nil {
		return decoded, nil
	}
	decoded, rawErr := base64.RawStdEncoding.DecodeString(data)
	if rawErr != nil {
		return nil, fmt.Errorf("invalid base64 payload")
	}
	return decoded, nil
}

// DetectImageMIMEType returns the MIME type sniffed from a base64 image payload.
func DetectImageMIMEType(data string) (string, error) {
	decoded, err := DecodeBase64Payload(data)
	if err != nil {
		return "", err
	}
	mimeType := http.DetectContentType(decoded)
	if !strings.HasPrefix(mimeType, "image/") {
		return "", fmt.Errorf("image payload has unrecognized format")
	}
	return mimeType, nil
}
