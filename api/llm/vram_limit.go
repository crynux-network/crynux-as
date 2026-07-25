package llm

import (
	"crynux_as/config"
	"crynux_as/service"
	"errors"
	"strconv"
	"strings"
)

// resolveUserVramLimit resolves the user-specified VRAM limit in GB. The URL
// path value overrides the body value. A nil result means the user did not
// specify a VRAM limit.
func resolveUserVramLimit(bodyVramLimit *uint64, pathVramLimit string) (*uint64, error) {
	trimmedPathVramLimit := strings.TrimSpace(pathVramLimit)
	if trimmedPathVramLimit != "" {
		pathVram, err := strconv.ParseUint(trimmedPathVramLimit, 10, 64)
		if err != nil {
			return nil, errors.New("vram_limit must be an unsigned integer")
		}
		return &pathVram, nil
	}
	return bodyVramLimit, nil
}

// resolveEffectiveVram resolves the effective VRAM in GB used for billing and
// Bridge forwarding: the user value when specified, else the loaded model's
// min_vram from the cache, else the configured default.
func resolveEffectiveVram(model string, userVram *uint64) uint64 {
	if userVram != nil {
		return *userVram
	}
	if loaded, ok := service.GetLoadedLLMModel(model); ok {
		return loaded.MinVRAM
	}
	return config.GetConfig().LLM.DefaultVramLimit
}
