package service

import (
	"crynux_as/llmadapter"
	"crynux_as/models"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	modelCacheTaskKindLLM   = "llm"
	modelCacheTaskKindImage = "image"
	modelCacheTaskKindVideo = "video"
	defaultLoraWeight       = "100"
	defaultVideoLoraTarget  = "auto"
)

// modelCacheIdentity is the normalized execution identity of one model
// configuration. The key fields hold canonical JSON. ComponentsKey is used by
// image and video; LoraKey holds lora for image and loras for video.
type modelCacheIdentity struct {
	TaskKind        string
	ModelID         string
	DType           string
	QuantizationKey string
	ComponentsKey   string
	LoraKey         string
}

// hash returns the lowercase hex SHA-256 of the canonical identity JSON.
func (i modelCacheIdentity) hash() (string, error) {
	identity := map[string]any{
		"task_kind":           i.TaskKind,
		"model_id":            i.ModelID,
		"dtype":               i.DType,
		"quantization_config": json.RawMessage(i.QuantizationKey),
	}
	switch i.TaskKind {
	case modelCacheTaskKindLLM:
	case modelCacheTaskKindImage:
		identity["components"] = json.RawMessage(i.ComponentsKey)
		identity["lora"] = json.RawMessage(i.LoraKey)
	case modelCacheTaskKindVideo:
		identity["components"] = json.RawMessage(i.ComponentsKey)
		identity["loras"] = json.RawMessage(i.LoraKey)
	default:
		return "", fmt.Errorf("unsupported model cache task_kind %q", i.TaskKind)
	}
	canonical, err := models.CanonicalJSON(identity)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// normalizeRepository lowercases Hugging Face repository names and keeps URLs unchanged.
func normalizeRepository(name string) string {
	return llmadapter.NormalizeModelName(name)
}

// normalizeComponentSource converts a component source to its identity shape:
// format, normalized repo, and format_kwargs when non-empty. The revision is not part of the identity.
func normalizeComponentSource(usage string, format, repo string, formatKwargs map[string]any) (map[string]any, error) {
	if usage == "" || format == "" || repo == "" {
		return nil, fmt.Errorf("component %q requires usage name, format and repo", usage)
	}
	source := map[string]any{
		"format": format,
		"repo":   normalizeRepository(repo),
	}
	if len(formatKwargs) > 0 {
		source["format_kwargs"] = formatKwargs
	}
	return source, nil
}

// normalizeTaskArgsComponents converts a TaskArgs components value to the
// identity shape and returns it with its canonical key. A nil value means no components.
func normalizeTaskArgsComponents(value any) (map[string]any, string, error) {
	components := map[string]any{}
	if value != nil {
		raw, ok := value.(map[string]any)
		if !ok {
			return nil, "", errors.New("components must be an object")
		}
		for usage, item := range raw {
			entry, ok := item.(map[string]any)
			if !ok {
				return nil, "", fmt.Errorf("component %q must be an object", usage)
			}
			format, _ := entry["format"].(string)
			repo, _ := entry["repo"].(string)
			var formatKwargs map[string]any
			if kwargs, present := entry["format_kwargs"]; present && kwargs != nil {
				if formatKwargs, ok = kwargs.(map[string]any); !ok {
					return nil, "", fmt.Errorf("component %q format_kwargs must be an object", usage)
				}
			}
			source, err := normalizeComponentSource(usage, format, repo, formatKwargs)
			if err != nil {
				return nil, "", err
			}
			components[usage] = source
		}
	}
	key, err := models.CanonicalJSON(components)
	if err != nil {
		return nil, "", err
	}
	return components, string(key), nil
}

// normalizeTaskArgsLora converts a TaskArgs lora value to the identity shape
// with defaults applied and returns it with its canonical key. A nil value means no LoRA.
func normalizeTaskArgsLora(value any) (map[string]any, string, error) {
	if value == nil {
		return nil, "null", nil
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, "", errors.New("lora must be an object or null")
	}
	model, _ := raw["model"].(string)
	if model == "" {
		return nil, "", errors.New("lora requires model")
	}
	weightFileName, _ := raw["weight_file_name"].(string)
	var weight any = json.Number(defaultLoraWeight)
	if raw["weight"] != nil {
		weight = raw["weight"]
	}
	lora := map[string]any{
		"model":            model,
		"weight_file_name": weightFileName,
		"weight":           weight,
	}
	key, err := models.CanonicalJSON(lora)
	if err != nil {
		return nil, "", err
	}
	return lora, string(key), nil
}

// normalizeTaskArgsVideoLoras converts a TaskArgs loras value to the identity
// shape with defaults applied and returns it with its canonical key. A nil
// value or an empty list means no LoRAs and yields an empty list.
func normalizeTaskArgsVideoLoras(value any) ([]any, string, error) {
	loras := []any{}
	if value != nil {
		raw, ok := value.([]any)
		if !ok {
			return nil, "", errors.New("loras must be an array")
		}
		for index, item := range raw {
			entry, ok := item.(map[string]any)
			if !ok {
				return nil, "", fmt.Errorf("loras[%d] must be an object", index)
			}
			model, _ := entry["model"].(string)
			if model == "" {
				return nil, "", fmt.Errorf("loras[%d] requires model", index)
			}
			weightFileName, _ := entry["weight_file_name"].(string)
			var weight any = json.Number(defaultLoraWeight)
			if entry["weight"] != nil {
				weight = entry["weight"]
			}
			target, _ := entry["target"].(string)
			if target == "" {
				target = defaultVideoLoraTarget
			}
			loras = append(loras, map[string]any{
				"model":            normalizeRepository(model),
				"weight_file_name": weightFileName,
				"weight":           weight,
				"target":           target,
			})
		}
	}
	key, err := models.CanonicalJSON(loras)
	if err != nil {
		return nil, "", err
	}
	return loras, string(key), nil
}
