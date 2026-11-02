package provider

import (
	"regexp"
	"strconv"
)

// anthropicImageTier is one Anthropic image resolution tier. Images are
// downscaled so the long edge fits maxEdge, then billed per 28px patch up to
// maxTokens.
type anthropicImageTier struct {
	maxEdge   float64
	maxTokens uint64
}

const anthropicPatchSize = 28

var (
	anthropicStandardTier = anthropicImageTier{maxEdge: 1568, maxTokens: 1568}
	anthropicHighResTier  = anthropicImageTier{maxEdge: 2576, maxTokens: 4784}
)

var (
	anthropicFamilyFirstModel  = regexp.MustCompile(`^claude-(opus|sonnet|haiku|fable)-(\d+)(?:-(\d{1,2}))?(?:-|$)`)
	anthropicVersionFirstModel = regexp.MustCompile(`^claude-(\d+)(?:-(\d{1,2}))?-(opus|sonnet|haiku)(?:-|$)`)
)

// resolveAnthropicImageTier selects the high-resolution tier for Fable models
// and Claude 4.7 or later, and the standard tier for Claude 3 through 4.6.
func resolveAnthropicImageTier(model string) (anthropicImageTier, error) {
	var family, majorText, minorText string
	if m := anthropicFamilyFirstModel.FindStringSubmatch(model); m != nil {
		family, majorText, minorText = m[1], m[2], m[3]
	} else if m := anthropicVersionFirstModel.FindStringSubmatch(model); m != nil {
		family, majorText, minorText = m[3], m[1], m[2]
	} else {
		return anthropicImageTier{}, unsupportedImageModelError(model)
	}
	major, _ := strconv.Atoi(majorText)
	minor := 0
	if minorText != "" {
		minor, _ = strconv.Atoi(minorText)
	}
	if major < 3 {
		return anthropicImageTier{}, unsupportedImageModelError(model)
	}
	if family == "fable" || major > 4 || (major == 4 && minor >= 7) {
		return anthropicHighResTier, nil
	}
	return anthropicStandardTier, nil
}

func (t anthropicImageTier) estimate(image CanonicalImageInfo) (uint64, error) {
	w, h := scaleToFit(image.Width, image.Height, t.maxEdge, t.maxEdge)
	tokens := ceilDiv(w, anthropicPatchSize) * ceilDiv(h, anthropicPatchSize)
	if tokens > t.maxTokens {
		tokens = t.maxTokens
	}
	return tokens, nil
}
