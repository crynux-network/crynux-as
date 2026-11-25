package service

import (
	"bytes"
	"crynux_as/config"
	"crynux_as/llmadapter"
	"crynux_as/models"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// isPresentQuantizationConfig reports whether userQuant carries a non-null
// quantization_config value. Empty or null json.RawMessage counts as absent.
func isPresentQuantizationConfig(userQuant any) bool {
	if userQuant == nil {
		return false
	}
	raw, ok := userQuant.(json.RawMessage)
	if !ok {
		return true
	}
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

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
// model is used. When the cache misses, dtype falls back to bfloat16 and
// quantization_config is null. MinVRAM is always filled: matched row, else the
// greatest min_vram for the model, else llm.default_vram_limit.
func ResolveLLMModelExecution(modelID string, userDtype string, userQuant any) ModelExecutionSelection {
	modelID = llmadapter.NormalizeModelID(modelID)
	userDtype = strings.ToLower(strings.TrimSpace(userDtype))
	hasUserDtype := userDtype != ""
	hasUserQuant := isPresentQuantizationConfig(userQuant)

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
				sel.Dtype = dtypeFromLoaded(loaded)
			}
			if !hasUserQuant {
				sel.QuantizationConfig = QuantizationConfigForAPI(loaded.QuantizationConfig)
			}
		} else {
			if !hasUserDtype {
				sel.Dtype = models.DTypeBFloat16
			}
			if !hasUserQuant {
				sel.QuantizationConfig = nil
			}
		}
		sel.MinVRAM = resolveLLMDefaultMinVRAM(modelID, sel.MinVRAM)
		return sel
	}

	if loaded, ok := GetLoadedLLMModel(modelID); ok {
		return ModelExecutionSelection{
			Dtype:              dtypeFromLoaded(loaded),
			QuantizationConfig: QuantizationConfigForAPI(loaded.QuantizationConfig),
			MinVRAM:            resolveLLMDefaultMinVRAM(modelID, loaded.MinVRAM),
			FromLoadedModels:   true,
		}
	}
	return ModelExecutionSelection{
		Dtype:              models.DTypeBFloat16,
		QuantizationConfig: nil,
		MinVRAM:            resolveLLMDefaultMinVRAM(modelID, 0),
	}
}

func resolveLLMDefaultMinVRAM(modelID string, matchedMinVRAM uint64) uint64 {
	if matchedMinVRAM > 0 {
		return matchedMinVRAM
	}
	if loaded, ok := GetLoadedLLMModel(modelID); ok && loaded.MinVRAM > 0 {
		return loaded.MinVRAM
	}
	return config.GetConfig().LLM.DefaultVramLimit
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
		return llmModelsCache.pick(func(model LoadedLLMModel) bool {
			return model.ModelID == modelID && model.QuantizationKey == quantKey
		})
	}
	if hasDtype {
		dtype = strings.ToLower(dtype)
		return llmModelsCache.pick(func(model LoadedLLMModel) bool {
			return model.ModelID == modelID && model.DType == dtype
		})
	}
	return GetLoadedLLMModel(modelID)
}

func dtypeFromLoaded(loaded LoadedLLMModel) models.DType {
	return models.DType(loaded.DType)
}

// ApplyLLMExecutionToTaskArgsJSON sets version, dtype, and quantization_config on TaskArgs JSON.
func ApplyLLMExecutionToTaskArgsJSON(taskArgsJSON string, sel ModelExecutionSelection) (string, error) {
	taskArgsJSON, err := NormalizeTaskArgsModelNames(taskArgsJSON, models.TaskTypeLLM)
	if err != nil {
		return "", err
	}
	payload, err := decodeJSONObjectUseNumber([]byte(taskArgsJSON))
	if err != nil {
		return "", err
	}
	payload["version"] = models.GPTTaskArgsVersion
	applyLLMExecution(payload, sel)
	out, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func applyLLMExecution(payload map[string]any, sel ModelExecutionSelection) {
	if sel.Dtype != "" {
		payload["dtype"] = string(sel.Dtype)
	}
	if !isPresentQuantizationConfig(sel.QuantizationConfig) {
		payload["quantization_config"] = nil
	} else {
		payload["quantization_config"] = sel.QuantizationConfig
	}
}

// ResolveRawLLMModelExecution resolves Raw LLM TaskArgs execution fields with
// the ResolveLLMModelExecution rules. A present dtype MUST be float16,
// bfloat16, or float32. The client TaskArgs version is kept unchanged.
func ResolveRawLLMModelExecution(taskArgsJSON string, modelID string) (TaskExecutionResolution, error) {
	taskArgsJSON, err := NormalizeTaskArgsModelNames(taskArgsJSON, models.TaskTypeLLM)
	if err != nil {
		return TaskExecutionResolution{}, err
	}
	payload, err := decodeJSONObjectUseNumber([]byte(taskArgsJSON))
	if err != nil {
		return TaskExecutionResolution{}, err
	}
	dtype, _, err := requestDType(payload)
	if err != nil {
		return TaskExecutionResolution{}, err
	}
	sel := ResolveLLMModelExecution(modelID, dtype, payload["quantization_config"])
	applyLLMExecution(payload, sel)
	out, err := json.Marshal(payload)
	if err != nil {
		return TaskExecutionResolution{}, err
	}
	return TaskExecutionResolution{
		TaskArgsJSON:     string(out),
		MinVRAM:          sel.MinVRAM,
		FromLoadedModels: sel.FromLoadedModels,
	}, nil
}

// ErrInvalidExecutionConfig means the dtype or another execution field of
// TaskArgs is invalid.
var ErrInvalidExecutionConfig = errors.New("invalid execution configuration")

func invalidExecutionConfig(err error) error {
	return fmt.Errorf("%w: %v", ErrInvalidExecutionConfig, err)
}

// diffusionRequestIdentity selects Image or Video loaded-models rows by cache
// identity. A nil field matches any row. The LoRA key is always compared: lora
// for Image rows and loras for Video rows.
type diffusionRequestIdentity struct {
	modelID       string
	dtype         *string
	quantKey      *string
	componentsKey *string
	loraKey       string
}

func (i diffusionRequestIdentity) matches(row LoadedLLMModel) bool {
	return row.ModelID == i.modelID &&
		row.identity().LoraKey == i.loraKey &&
		(i.dtype == nil || row.DType == *i.dtype) &&
		(i.quantKey == nil || row.QuantizationKey == *i.quantKey) &&
		(i.componentsKey == nil || row.ComponentsKey == *i.componentsKey)
}

// requestDType returns the concrete client dtype and whether the dtype key is
// present. A present dtype MUST be float16, bfloat16, or float32.
func requestDType(payload map[string]any) (string, bool, error) {
	raw, ok := payload["dtype"]
	if !ok {
		return "", false, nil
	}
	dtypeStr, isString := raw.(string)
	if !isString {
		return "", true, invalidExecutionConfig(errors.New("dtype must be float16, bfloat16, or float32"))
	}
	dtype, err := models.ParseConcreteDType(dtypeStr)
	if err != nil {
		return "", true, invalidExecutionConfig(err)
	}
	return string(dtype), true, nil
}

// newDiffusionRequestIdentity derives the cache identity of Image or Video
// TaskArgs. When exact is true, the TaskArgs MUST carry a concrete dtype and
// omitted quantization_config and components take their TaskArgs defaults;
// otherwise omitted fields match any row.
func newDiffusionRequestIdentity(taskKind, modelID string, payload map[string]any, exact bool) (diffusionRequestIdentity, error) {
	identity := diffusionRequestIdentity{modelID: llmadapter.NormalizeModelID(modelID)}

	dtype, hasDtype, err := requestDType(payload)
	if err != nil {
		return diffusionRequestIdentity{}, err
	}
	if hasDtype {
		identity.dtype = &dtype
	} else if exact {
		return diffusionRequestIdentity{}, invalidExecutionConfig(errors.New("dtype is required"))
	}

	if quant, ok := payload["quantization_config"]; ok || exact {
		quantKey, err := models.QuantizationConfigKey(quant)
		if err != nil {
			return diffusionRequestIdentity{}, invalidExecutionConfig(fmt.Errorf("invalid quantization_config: %w", err))
		}
		identity.quantKey = &quantKey
	}

	if components, ok := payload["components"]; ok || exact {
		_, componentsKey, err := normalizeTaskArgsComponents(components)
		if err != nil {
			return diffusionRequestIdentity{}, invalidExecutionConfig(err)
		}
		identity.componentsKey = &componentsKey
	}

	if taskKind == modelCacheTaskKindVideo {
		_, identity.loraKey, err = normalizeTaskArgsVideoLoras(payload["loras"])
	} else {
		_, identity.loraKey, err = normalizeTaskArgsLora(payload["lora"])
	}
	if err != nil {
		return diffusionRequestIdentity{}, invalidExecutionConfig(err)
	}
	return identity, nil
}

// taskArgsModelCacheHash returns the concrete dtype and the local model cache
// hash of resolved TaskArgs. The TaskArgs MUST carry a concrete dtype.
func taskArgsModelCacheHash(taskKind, modelID string, payload map[string]any) (string, string, error) {
	identity := modelCacheIdentity{TaskKind: taskKind, ModelID: llmadapter.NormalizeModelID(modelID)}
	if taskKind == modelCacheTaskKindLLM {
		dtype, hasDtype, err := requestDType(payload)
		if err != nil {
			return "", "", err
		}
		if !hasDtype {
			return "", "", invalidExecutionConfig(errors.New("dtype is required"))
		}
		quantKey, err := models.QuantizationConfigKey(payload["quantization_config"])
		if err != nil {
			return "", "", invalidExecutionConfig(fmt.Errorf("invalid quantization_config: %w", err))
		}
		identity.DType = dtype
		identity.QuantizationKey = quantKey
	} else {
		request, err := newDiffusionRequestIdentity(taskKind, modelID, payload, true)
		if err != nil {
			return "", "", err
		}
		identity.DType = *request.dtype
		identity.QuantizationKey = *request.quantKey
		identity.ComponentsKey = *request.componentsKey
		identity.LoraKey = request.loraKey
	}
	hash, err := identity.hash()
	if err != nil {
		return "", "", err
	}
	return identity.DType, hash, nil
}

// TaskExecutionResolution is the result of resolving Image or Video TaskArgs execution fields.
type TaskExecutionResolution struct {
	TaskArgsJSON     string
	MinVRAM          uint64
	FromLoadedModels bool
}

// ResolveImageModelExecution resolves Image TaskArgs execution fields for Raw,
// generations, and edits. See resolveDiffusionModelExecution for the rules.
func ResolveImageModelExecution(taskArgsJSON string, modelID string) (TaskExecutionResolution, error) {
	return resolveDiffusionModelExecution(models.TaskTypeImage, modelCacheTaskKindImage, taskArgsJSON, modelID, pickLoadedSD)
}

// ResolveVideoModelExecution resolves Video TaskArgs execution fields for Raw
// tasks. See resolveDiffusionModelExecution for the rules.
func ResolveVideoModelExecution(taskArgsJSON string, modelID string) (TaskExecutionResolution, error) {
	return resolveDiffusionModelExecution(models.TaskTypeVideo, modelCacheTaskKindVideo, taskArgsJSON, modelID, pickLoadedVideo)
}

// resolveDiffusionModelExecution applies the dtype rules to Image or Video TaskArgs.
//
// When the dtype key is present it MUST be float16, bfloat16, or float32 and all
// execution fields are kept as submitted; only an exact full-identity hit may
// supply min_vram.
//
// When the dtype key is omitted, the loaded-models row with the greatest
// min_vram matching the model, the submitted quantization_config and
// components, and the submitted LoRAs is selected. A hit writes the row dtype
// and fills omitted quantization_config and non-empty components from that
// row. A miss writes bfloat16 and keeps the submitted fields.
//
// min_vram defaults to llm.default_vram_limit when no row supplies it.
func resolveDiffusionModelExecution(
	taskType models.TaskType,
	taskKind string,
	taskArgsJSON string,
	modelID string,
	pick func(diffusionRequestIdentity) (LoadedLLMModel, bool),
) (TaskExecutionResolution, error) {
	taskArgsJSON, err := NormalizeTaskArgsModelNames(taskArgsJSON, taskType)
	if err != nil {
		return TaskExecutionResolution{}, err
	}
	payload, err := decodeJSONObjectUseNumber([]byte(taskArgsJSON))
	if err != nil {
		return TaskExecutionResolution{}, err
	}
	if taskType == models.TaskTypeImage {
		if version, ok := payload["version"].(string); !ok || version == "" {
			payload["version"] = models.GPTTaskArgsVersion
		}
	}
	minVRAM := config.GetConfig().LLM.DefaultVramLimit

	dtype, hasDtype, err := requestDType(payload)
	if err != nil {
		return TaskExecutionResolution{}, err
	}
	var loaded LoadedLLMModel
	var fromLoaded bool
	if hasDtype {
		payload["dtype"] = dtype
		identity, err := newDiffusionRequestIdentity(taskKind, modelID, payload, true)
		if err != nil {
			return TaskExecutionResolution{}, err
		}
		loaded, fromLoaded = pick(identity)
	} else {
		identity, err := newDiffusionRequestIdentity(taskKind, modelID, payload, false)
		if err != nil {
			return TaskExecutionResolution{}, err
		}
		loaded, fromLoaded = pick(identity)
		if fromLoaded {
			payload["dtype"] = loaded.DType
			if _, ok := payload["quantization_config"]; !ok {
				payload["quantization_config"] = cloneRawOrNull(loaded.QuantizationConfig)
			}
			if _, ok := payload["components"]; !ok && len(loaded.Components) > 0 {
				payload["components"] = loaded.Components
			}
		} else {
			payload["dtype"] = string(models.DTypeBFloat16)
		}
	}
	if fromLoaded && loaded.MinVRAM > 0 {
		minVRAM = loaded.MinVRAM
	}

	out, err := json.Marshal(payload)
	if err != nil {
		return TaskExecutionResolution{}, err
	}
	return TaskExecutionResolution{
		TaskArgsJSON:     string(out),
		MinVRAM:          minVRAM,
		FromLoadedModels: fromLoaded,
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
