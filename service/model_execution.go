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

// ErrImageExecutionUnresolved means Image TaskArgs with omitted or auto dtype could not
// be resolved to one coherent loaded-models row with a concrete execution dtype.
var ErrImageExecutionUnresolved = errors.New("unresolved image execution configuration")

// ImageExecutionResolution is the result of resolving Image TaskArgs execution fields.
type ImageExecutionResolution struct {
	TaskArgsJSON     string
	MinVRAM          uint64
	FromLoadedModels bool
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

func isConcreteExecutionDType(dtype string) bool {
	switch strings.ToLower(strings.TrimSpace(dtype)) {
	case string(models.DTypeFloat16), string(models.DTypeBFloat16), string(models.DTypeFloat32):
		return true
	default:
		return false
	}
}

func unresolvedImageExecution(msg string) error {
	return fmt.Errorf("%w: %s", ErrImageExecutionUnresolved, msg)
}

// requestImageDType returns the normalized client dtype and whether the request is in
// concrete mode. Omitted dtype and "auto" are auto mode.
func requestImageDType(payload map[string]any) (dtype string, concrete bool, err error) {
	raw, ok := payload["dtype"]
	if !ok {
		return autoExecutionDType, false, nil
	}
	dtypeStr, isString := raw.(string)
	if !isString {
		return "", false, errors.New("dtype must be a string")
	}
	normalized := normalizeExecutionDType(dtypeStr)
	if isConcreteExecutionDType(normalized) {
		return normalized, true, nil
	}
	if normalized == autoExecutionDType {
		return autoExecutionDType, false, nil
	}
	return "", false, unresolvedImageExecution("dtype must be float16, bfloat16, float32, or auto")
}

// ResolveImageModelExecution resolves Image TaskArgs execution fields for Raw, generations,
// and edits. Concrete dtype (float16, bfloat16, float32) is kept as submitted and does not
// require a loaded-models hit; only an exact full-identity hit may supply min_vram.
// Omitted or auto dtype must match one coherent loaded-models row; the selected row's
// concrete execution_dtype and any omitted quantization_config/components are written from
// that same row. A request without lora never selects a LoRA row.
func ResolveImageModelExecution(taskArgsJSON string, modelID string) (ImageExecutionResolution, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(taskArgsJSON), &payload); err != nil {
		return ImageExecutionResolution{}, err
	}
	payload["version"] = models.GPTTaskArgsVersion
	minVRAM := config.GetConfig().LLM.DefaultVramLimit

	dtype, concrete, err := requestImageDType(payload)
	if err != nil {
		return ImageExecutionResolution{}, err
	}

	if concrete {
		payload["dtype"] = dtype
		identity, err := newImageRequestIdentity(modelID, payload, true)
		if err != nil {
			return ImageExecutionResolution{}, err
		}
		fromLoaded := false
		if loaded, ok := pickLoadedSD(identity); ok {
			fromLoaded = true
			if loaded.MinVRAM > 0 {
				minVRAM = loaded.MinVRAM
			}
		}
		out, err := json.Marshal(payload)
		if err != nil {
			return ImageExecutionResolution{}, err
		}
		return ImageExecutionResolution{
			TaskArgsJSON:     string(out),
			MinVRAM:          minVRAM,
			FromLoadedModels: fromLoaded,
		}, nil
	}

	identity, err := newImageRequestIdentity(modelID, payload, false)
	if err != nil {
		return ImageExecutionResolution{}, err
	}
	// Auto mode resolves dtype from the selected row; do not filter by client "auto".
	identity.dtype = nil
	loaded, ok := pickLoadedSD(identity)
	if !ok {
		return ImageExecutionResolution{}, unresolvedImageExecution(
			"no matching loaded image model configuration for the requested execution settings",
		)
	}
	if !isConcreteExecutionDType(loaded.ExecutionDType) {
		return ImageExecutionResolution{}, unresolvedImageExecution(
			"matched loaded image model configuration does not have a concrete execution dtype",
		)
	}
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

	out, err := json.Marshal(payload)
	if err != nil {
		return ImageExecutionResolution{}, err
	}
	return ImageExecutionResolution{
		TaskArgsJSON:     string(out),
		MinVRAM:          minVRAM,
		FromLoadedModels: true,
	}, nil
}

// ApplyImageExecutionToTaskArgsJSON resolves Image TaskArgs execution fields and returns
// the canonical JSON with the default min_vram for billing when the user did not set one.
func ApplyImageExecutionToTaskArgsJSON(taskArgsJSON string, modelID string) (string, uint64, error) {
	resolved, err := ResolveImageModelExecution(taskArgsJSON, modelID)
	if err != nil {
		return "", 0, err
	}
	return resolved.TaskArgsJSON, resolved.MinVRAM, nil
}
