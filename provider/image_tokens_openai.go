package provider

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

const (
	openAIPatchSize           = 32
	openAITileSize            = 512
	openAITileMaxEdge         = 2048
	openAITileShortEdge       = 768
	openAIRejectPatchLimit    = 30000
	openAILowBoxEdge          = 512
	openAICommonMaxEdge       = 2048
	openAIAstraMaxEdge        = 65535
	openAIOriginalMaxEdge     = 65535
	openAIOriginalAltMaxEdge  = 6000
	openAIHighPatchBudget     = 2500
	openAISharedPatchBudget   = 6144
	openAIOriginalPatchBudget = 10000
)

var openAIDateSuffix = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)

// openAIDetailLimits is the resizing behavior for one OpenAI detail level.
type openAIDetailLimits struct {
	// maxEdge is the maximum pixel length of either side after aspect-preserving
	// scale-down. Zero means no edge limit.
	maxEdge float64
	// patchBudget is the maximum number of 32px patches after resizing. Zero
	// means the image is not resized to meet a patch budget.
	patchBudget uint64
	// rejectAbovePatches rejects the request when the resized image still needs
	// more than this many patches. Zero disables the check.
	rejectAbovePatches uint64
}

// openAIImageRule is one OpenAI image token rule.
type openAIImageRule struct {
	// patchMultiplier > 0 selects patch-based tokenization.
	patchMultiplier float64
	// baseTokens and tileTokens select tile-based tokenization.
	baseTokens uint64
	tileTokens uint64
	// details maps a normalized detail value to patch resizing limits.
	// Empty detail is treated as "auto". Unsupported values return an error.
	details map[string]openAIDetailLimits
}

func resolveOpenAIImageRule(model string) (openAIImageRule, error) {
	name := stripOpenAIDateSuffix(strings.ToLower(strings.TrimSpace(model)))
	switch {
	case name == "o1-mini" || hasModelPrefix(name, "o1-mini"):
		return openAIImageRule{}, unsupportedImageModelError(model)
	case name == "o3-mini" || hasModelPrefix(name, "o3-mini"):
		return openAIImageRule{}, unsupportedImageModelError(model)
	}

	if rule, ok := openAIExactPatchRules[name]; ok {
		return rule, nil
	}
	if rule, ok := openAIExactTileRules[name]; ok {
		return rule, nil
	}

	// Longer prefixes first so gpt-5.4-mini does not match gpt-5.4.
	for _, entry := range openAIPrefixPatchRules {
		if hasModelPrefix(name, entry.prefix) {
			return entry.rule, nil
		}
	}
	for _, entry := range openAIPrefixTileRules {
		if hasModelPrefix(name, entry.prefix) {
			return entry.rule, nil
		}
	}
	return openAIImageRule{}, unsupportedImageModelError(model)
}

func stripOpenAIDateSuffix(model string) string {
	return openAIDateSuffix.ReplaceAllString(model, "")
}

func openAIPatchRule(multiplier float64, details map[string]openAIDetailLimits) openAIImageRule {
	return openAIImageRule{patchMultiplier: multiplier, details: details}
}

func openAITileRule(base, tile uint64) openAIImageRule {
	return openAIImageRule{
		baseTokens: base,
		tileTokens: tile,
		details: map[string]openAIDetailLimits{
			"low":  {},
			"high": {},
			"auto": {},
		},
	}
}

var (
	openAIDetailLow512 = openAIDetailLimits{maxEdge: openAILowBoxEdge}
	openAIDetailShared = openAIDetailLimits{
		maxEdge:     openAICommonMaxEdge,
		patchBudget: openAISharedPatchBudget,
	}
	openAIDetailHigh2500Edge2048 = openAIDetailLimits{
		maxEdge:     openAICommonMaxEdge,
		patchBudget: openAIHighPatchBudget,
	}
	openAIDetailHigh2500Edge65535 = openAIDetailLimits{
		maxEdge:     openAIAstraMaxEdge,
		patchBudget: openAIHighPatchBudget,
	}
	openAIDetailOriginalPreserve = openAIDetailLimits{
		maxEdge:            openAIOriginalMaxEdge,
		rejectAbovePatches: openAIRejectPatchLimit,
	}
	openAIDetailOriginalAlt = openAIDetailLimits{
		maxEdge:     openAIOriginalAltMaxEdge,
		patchBudget: openAIOriginalPatchBudget,
	}
)

var openAIExactPatchRules = map[string]openAIImageRule{
	"gpt-6-astra": openAIPatchRule(1.2, map[string]openAIDetailLimits{
		"low":      openAIDetailLow512,
		"high":     openAIDetailHigh2500Edge65535,
		"original": openAIDetailOriginalPreserve,
		"auto":     openAIDetailOriginalPreserve,
	}),
	"gpt-6.1-sol": openAIPatchRule(1.2, map[string]openAIDetailLimits{
		"low":      openAIDetailLow512,
		"high":     openAIDetailHigh2500Edge65535,
		"original": openAIDetailOriginalPreserve,
		"auto":     openAIDetailOriginalPreserve,
	}),
	"gpt-6.1-terra": openAIPatchRule(1.2, map[string]openAIDetailLimits{
		"low":      openAIDetailLow512,
		"high":     openAIDetailHigh2500Edge65535,
		"original": openAIDetailOriginalPreserve,
		"auto":     openAIDetailOriginalPreserve,
	}),
	"gpt-6-luna": openAIPatchRule(1.2, map[string]openAIDetailLimits{
		"low":      openAIDetailLow512,
		"high":     openAIDetailHigh2500Edge65535,
		"original": openAIDetailOriginalPreserve,
		"auto":     openAIDetailOriginalPreserve,
	}),
	"gpt-5.6-sol": openAIPatchRule(1.2, map[string]openAIDetailLimits{
		"low":      openAIDetailLow512,
		"high":     openAIDetailHigh2500Edge2048,
		"original": openAIDetailOriginalPreserve,
		"auto":     openAIDetailOriginalPreserve,
	}),
	"gpt-5.6-terra": openAIPatchRule(1.2, map[string]openAIDetailLimits{
		"low":      openAIDetailLow512,
		"high":     openAIDetailHigh2500Edge2048,
		"original": openAIDetailOriginalPreserve,
		"auto":     openAIDetailOriginalPreserve,
	}),
	"gpt-5.6-luna": openAIPatchRule(1.2, map[string]openAIDetailLimits{
		"low":      openAIDetailLow512,
		"high":     openAIDetailHigh2500Edge2048,
		"original": openAIDetailOriginalPreserve,
		"auto":     openAIDetailOriginalPreserve,
	}),
	"gpt-5.5": openAIPatchRule(1.2, map[string]openAIDetailLimits{
		"low":      openAIDetailLow512,
		"high":     openAIDetailHigh2500Edge2048,
		"original": openAIDetailOriginalAlt,
		"auto":     openAIDetailOriginalAlt,
	}),
	"gpt-5.2": openAIPatchRule(1.2, map[string]openAIDetailLimits{
		"low":  openAIDetailShared,
		"high": openAIDetailShared,
		"auto": openAIDetailShared,
	}),
}

var openAIExactTileRules = map[string]openAIImageRule{
	"gpt-5":   openAITileRule(70, 140),
	"gpt-5.1": openAITileRule(70, 140),
}

var openAIPrefixPatchRules = []struct {
	prefix string
	rule   openAIImageRule
}{
	{"gpt-5.4-nano", openAIPatchRule(1.2, map[string]openAIDetailLimits{
		"low":      openAIDetailShared,
		"high":     openAIDetailHigh2500Edge2048,
		"original": openAIDetailOriginalAlt,
		"auto":     openAIDetailHigh2500Edge2048,
	})},
	{"gpt-5.4-mini", openAIPatchRule(1.2, map[string]openAIDetailLimits{
		"low":      openAIDetailShared,
		"high":     openAIDetailHigh2500Edge2048,
		"original": openAIDetailOriginalAlt,
		"auto":     openAIDetailHigh2500Edge2048,
	})},
	{"gpt-5.4", openAIPatchRule(1.2, map[string]openAIDetailLimits{
		"low":      openAIDetailShared,
		"high":     openAIDetailHigh2500Edge2048,
		"original": openAIDetailOriginalAlt,
		"auto":     openAIDetailHigh2500Edge2048,
	})},
	{"gpt-5-mini", openAIPatchRule(1.2, map[string]openAIDetailLimits{
		"low":  openAIDetailShared,
		"high": openAIDetailShared,
		"auto": openAIDetailShared,
	})},
	{"gpt-5-nano", openAIPatchRule(1.5, map[string]openAIDetailLimits{
		"low":  openAIDetailShared,
		"high": openAIDetailShared,
		"auto": openAIDetailShared,
	})},
	{"gpt-4.1-mini", openAIPatchRule(1.62, map[string]openAIDetailLimits{
		"low":  openAIDetailShared,
		"high": openAIDetailShared,
		"auto": openAIDetailShared,
	})},
	{"gpt-4.1-nano", openAIPatchRule(2.46, map[string]openAIDetailLimits{
		"low":  openAIDetailShared,
		"high": openAIDetailShared,
		"auto": openAIDetailShared,
	})},
	{"o4-mini", openAIPatchRule(1.72, map[string]openAIDetailLimits{
		"low":  openAIDetailShared,
		"high": openAIDetailShared,
		"auto": openAIDetailShared,
	})},
}

var openAIPrefixTileRules = []struct {
	prefix string
	rule   openAIImageRule
}{
	{"gpt-4o-mini", openAITileRule(2833, 5667)},
	{"chatgpt-4o", openAITileRule(85, 170)},
	{"gpt-4o", openAITileRule(85, 170)},
	{"gpt-4.1", openAITileRule(85, 170)},
	{"gpt-4.5", openAITileRule(85, 170)},
	{"gpt-4-turbo", openAITileRule(85, 170)},
	{"o1-pro", openAITileRule(75, 150)},
	{"o1", openAITileRule(75, 150)},
	{"o3", openAITileRule(75, 150)},
}

func (r openAIImageRule) estimate(image CanonicalImageInfo) (uint64, error) {
	detail := image.Detail
	if detail == "" {
		detail = "auto"
	}
	limits, ok := r.details[detail]
	if !ok {
		return 0, fmt.Errorf("image detail %q is not supported for this model", detail)
	}
	if r.patchMultiplier > 0 {
		patches, err := openAIPatchCount(image.Width, image.Height, limits)
		if err != nil {
			return 0, err
		}
		return uint64(math.Ceil(float64(patches) * r.patchMultiplier)), nil
	}
	if detail == "low" {
		return r.baseTokens, nil
	}
	return r.baseTokens + r.tileTokens*openAITileCount(image.Width, image.Height), nil
}

// openAIPatchCount fits the image within the detail edge limit, then within the
// patch budget when one is set, and rejects images above the hard patch limit.
func openAIPatchCount(width, height int, limits openAIDetailLimits) (uint64, error) {
	w, h := float64(width), float64(height)
	if limits.maxEdge > 0 {
		w, h = scaleToFit(width, height, limits.maxEdge, limits.maxEdge)
	}
	patches := ceilDiv(w, openAIPatchSize) * ceilDiv(h, openAIPatchSize)
	if limits.patchBudget > 0 && patches > limits.patchBudget {
		r := math.Sqrt(openAIPatchSize * openAIPatchSize * float64(limits.patchBudget) / (w * h))
		scaledW, scaledH := w*r/openAIPatchSize, h*r/openAIPatchSize
		r *= math.Min(math.Floor(scaledW)/scaledW, math.Floor(scaledH)/scaledH)
		w, h = w*r, h*r
		patches = ceilDiv(w, openAIPatchSize) * ceilDiv(h, openAIPatchSize)
		if patches > limits.patchBudget {
			patches = limits.patchBudget
		}
	}
	if limits.rejectAbovePatches > 0 && patches > limits.rejectAbovePatches {
		return 0, fmt.Errorf("image requires %d patches which exceeds the %d-patch limit", patches, limits.rejectAbovePatches)
	}
	return patches, nil
}

// openAITileCount fits the image within 2048x2048, downscales the shortest side
// to 768px when larger, and counts 512px tiles.
func openAITileCount(width, height int) uint64 {
	w, h := scaleToFit(width, height, openAITileMaxEdge, openAITileMaxEdge)
	if w <= h && w > openAITileShortEdge {
		w, h = openAITileShortEdge, h*openAITileShortEdge/w
	} else if h < w && h > openAITileShortEdge {
		w, h = w*openAITileShortEdge/h, openAITileShortEdge
	}
	return ceilDiv(w, openAITileSize) * ceilDiv(h, openAITileSize)
}
