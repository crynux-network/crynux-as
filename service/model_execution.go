package service

import (
	"crynux_as/llmadapter"
	"crynux_as/models"
	"encoding/json"
	"strings"
)

type ModelExecutionSelection struct {
	Dtype              models.DType
	QuantizationConfig any
	MinVRAM            uint64
	FromLoadedModels   bool
}

// ResolveLLMModelExecution picks dtype and quantization for an OpenAI LLM request.
// When the client supplies dtype and/or quantization_config, those values are used
// (missing side filled from the matching loaded-models row when present, else defaults).
// When neither is supplied, the loaded-models row with the greatest min_vram for the
// model is used. When the cache misses, dtype falls back to the model default and
// quantization_config is null.
func ResolveLLMModelExecution(modelID string, userDtype string, userQuant any) ModelExecutionSelection {
	modelID = llmadapter.NormalizeModelID(modelID)
	userDtype = strings.ToLower(strings.TrimSpace(userDtype))
	hasUserDtype := userDtype != ""
	hasUserQuant := userQuant != nil

	if hasUserDtype || hasUserQuant {
		sel := ModelExecutionSelection{FromLoadedModels: false}
		if hasUserDtype {
			sel.Dtype = models.DType(userDtype)
		}
		if hasUserQuant {
			sel.QuantizationConfig = userQuant
		}
		if loaded, ok := lookupExactOrMax(modelID, sel.QuantizationConfig, string(sel.Dtype), hasUserQuant, hasUserDtype); ok {
			sel.MinVRAM = loaded.MinVRAM
			sel.FromLoadedModels = true
			if !hasUserDtype {
				sel.Dtype = dtypeFromLoaded(loaded, modelID)
			}
			if !hasUserQuant {
				sel.QuantizationConfig = QuantizationConfigForAPI(loaded.QuantizationConfig)
			}
		} else {
			if !hasUserDtype {
				sel.Dtype = resolveDefaultDType(modelID)
			}
			if !hasUserQuant {
				sel.QuantizationConfig = nil
			}
		}
		return sel
	}

	if loaded, ok := GetLoadedLLMModel(modelID); ok {
		return ModelExecutionSelection{
			Dtype:              dtypeFromLoaded(loaded, modelID),
			QuantizationConfig: QuantizationConfigForAPI(loaded.QuantizationConfig),
			MinVRAM:            loaded.MinVRAM,
			FromLoadedModels:   true,
		}
	}
	return ModelExecutionSelection{
		Dtype:              resolveDefaultDType(modelID),
		QuantizationConfig: nil,
	}
}

func lookupExactOrMax(modelID string, quant any, dtype string, hasQuant, hasDtype bool) (LoadedLLMModel, bool) {
	if hasQuant && hasDtype {
		return GetLoadedLLMModelExact(modelID, quant, dtype)
	}
	if hasQuant {
		quantKey, err := models.QuantizationConfigKey(quant)
		if err != nil {
			return LoadedLLMModel{}, false
		}
		llmModelsCache.mu.RLock()
		defer llmModelsCache.mu.RUnlock()
		var best LoadedLLMModel
		found := false
		for _, model := range llmModelsCache.models {
			if model.ModelID != modelID || model.QuantizationKey != quantKey {
				continue
			}
			if !found || betterMaxMinVRAM(model, best) {
				best = model
				found = true
			}
		}
		return best, found
	}
	if hasDtype {
		llmModelsCache.mu.RLock()
		defer llmModelsCache.mu.RUnlock()
		var best LoadedLLMModel
		found := false
		dtype = strings.ToLower(dtype)
		for _, model := range llmModelsCache.models {
			if model.ModelID != modelID || model.ExecutionDType != dtype {
				continue
			}
			if !found || betterMaxMinVRAM(model, best) {
				best = model
				found = true
			}
		}
		return best, found
	}
	return GetLoadedLLMModel(modelID)
}

func dtypeFromLoaded(loaded LoadedLLMModel, modelID string) models.DType {
	if loaded.ExecutionDType != "" {
		return models.DType(loaded.ExecutionDType)
	}
	return resolveDefaultDType(modelID)
}

func resolveDefaultDType(modelID string) models.DType {
	if strings.HasPrefix(modelID, "qwen/qwen2.5") {
		return models.DTypeBFloat16
	}
	return models.DTypeAuto
}

// ApplyLLMExecutionToTaskArgsJSON sets version, dtype, and quantization_config on TaskArgs JSON.
func ApplyLLMExecutionToTaskArgsJSON(taskArgsJSON string, sel ModelExecutionSelection) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(taskArgsJSON), &payload); err != nil {
		return "", err
	}
	payload["version"] = models.GPTTaskArgsVersion
	if sel.Dtype != "" {
		payload["dtype"] = string(sel.Dtype)
	}
	if sel.QuantizationConfig == nil {
		payload["quantization_config"] = nil
	} else {
		payload["quantization_config"] = sel.QuantizationConfig
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ResolveImageDefaultMinVRAM returns the max-min_vram loaded image row for modelID.
func ResolveImageDefaultMinVRAM(modelID string) (uint64, bool) {
	loaded, ok := GetLoadedSDModel(modelID)
	if !ok || loaded.MinVRAM == 0 {
		return 0, false
	}
	return loaded.MinVRAM, true
}
