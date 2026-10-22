package service

import (
	"crynux_as/config"
	"crynux_as/llmadapter"
	"crynux_as/models"
	"encoding/json"
	"errors"
	"fmt"
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
		return pickBest(llmModelsCache.models, func(model LoadedLLMModel) bool {
			return model.ModelID == modelID && model.QuantizationKey == quantKey
		})
	}
	if hasDtype {
		dtype = strings.ToLower(dtype)
		llmModelsCache.mu.RLock()
		defer llmModelsCache.mu.RUnlock()
		return pickBest(llmModelsCache.models, func(model LoadedLLMModel) bool {
			return model.ModelID == modelID && model.ExecutionDType == dtype
		})
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

// imageRequestIdentity selects Image loaded-models rows by cache identity.
// A nil field matches any row; the LoRA key is always compared.
type imageRequestIdentity struct {
	modelID       string
	dtype         *string
	quantKey      *string
	componentsKey *string
	loraKey       string
}

func (i imageRequestIdentity) matches(row LoadedLLMModel) bool {
	return row.ModelID == i.modelID &&
		row.LoraKey == i.loraKey &&
		(i.dtype == nil || row.ExecutionDType == *i.dtype) &&
		(i.quantKey == nil || row.QuantizationKey == *i.quantKey) &&
		(i.componentsKey == nil || row.ComponentsKey == *i.componentsKey)
}

// newImageRequestIdentity derives the cache identity of Image TaskArgs. When exact
// is true, omitted dtype, quantization_config, and components take their TaskArgs
// defaults; otherwise omitted fields match any row.
func newImageRequestIdentity(modelID string, payload map[string]any, exact bool) (imageRequestIdentity, error) {
	identity := imageRequestIdentity{modelID: llmadapter.NormalizeModelID(modelID)}

	if dtype, ok := payload["dtype"]; ok {
		dtypeStr, isString := dtype.(string)
		if !isString {
			return imageRequestIdentity{}, errors.New("dtype must be a string")
		}
		normalized := normalizeExecutionDType(dtypeStr)
		identity.dtype = &normalized
	} else if exact {
		normalized := autoExecutionDType
		identity.dtype = &normalized
	}

	if quant, ok := payload["quantization_config"]; ok || exact {
		quantKey, err := models.QuantizationConfigKey(quant)
		if err != nil {
			return imageRequestIdentity{}, fmt.Errorf("invalid quantization_config: %w", err)
		}
		identity.quantKey = &quantKey
	}

	if components, ok := payload["components"]; ok || exact {
		_, componentsKey, err := normalizeTaskArgsComponents(components)
		if err != nil {
			return imageRequestIdentity{}, err
		}
		identity.componentsKey = &componentsKey
	}

	_, loraKey, err := normalizeTaskArgsLora(payload["lora"])
	if err != nil {
		return imageRequestIdentity{}, err
	}
	identity.loraKey = loraKey
	return identity, nil
}

// GetLoadedSDModelExact returns the Image row whose full cache identity (dtype,
// quantization_config, components, and lora) equals that of taskArgsJSON.
func GetLoadedSDModelExact(modelID string, taskArgsJSON string) (LoadedLLMModel, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(taskArgsJSON), &payload); err != nil {
		return LoadedLLMModel{}, false, err
	}
	identity, err := newImageRequestIdentity(modelID, payload, true)
	if err != nil {
		return LoadedLLMModel{}, false, err
	}
	loaded, ok := pickLoadedSD(identity)
	return loaded, ok, nil
}

// ApplyImageExecutionToTaskArgsJSON sets version and cache execution fields on Image TaskArgs.
// The selected row is the greatest-min_vram row whose cache identity agrees with every
// client-supplied quantization_config, components, and lora; a request without lora never
// selects a LoRA row. dtype and every omitted field are taken from that same row. When no
// row matches, the client fields are kept and the default VRAM limit applies.
func ApplyImageExecutionToTaskArgsJSON(taskArgsJSON string, modelID string) (string, uint64, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(taskArgsJSON), &payload); err != nil {
		return "", 0, err
	}
	payload["version"] = models.GPTTaskArgsVersion
	minVRAM := config.GetConfig().LLM.DefaultVramLimit

	identity, err := newImageRequestIdentity(modelID, payload, false)
	if err != nil {
		return "", 0, err
	}
	if loaded, ok := pickLoadedSD(identity); ok {
		if loaded.MinVRAM > 0 {
			minVRAM = loaded.MinVRAM
		}
		payload["dtype"] = loaded.ExecutionDType
		if _, ok := payload["quantization_config"]; !ok {
			payload["quantization_config"] = QuantizationConfigForAPI(loaded.QuantizationConfig)
		}
		if _, ok := payload["components"]; !ok && len(loaded.Components) > 0 {
			payload["components"] = loaded.Components
		}
		if _, ok := payload["lora"]; !ok && loaded.Lora != nil {
			payload["lora"] = loaded.Lora
		}
	} else if _, ok := payload["quantization_config"]; !ok {
		payload["quantization_config"] = nil
	}

	out, err := json.Marshal(payload)
	if err != nil {
		return "", 0, err
	}
	return string(out), minVRAM, nil
}
