package service

import (
	"bytes"
	"crynux_as/models"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	modelCacheTaskKindLLM   = "llm"
	modelCacheTaskKindImage = "image"
	modelCacheHashBytes     = 32
	defaultLoraWeight       = "100"
	autoExecutionDType      = "auto"
)

type modelCacheEntry struct {
	Repository   string         `json:"repository"`
	Format       string         `json:"format"`
	FormatKwargs map[string]any `json:"format_kwargs"`
}

// modelCacheConfig is the canonical config Relay derives model_cache_hash from.
type modelCacheConfig struct {
	TaskKind           string                     `json:"task_kind"`
	Model              *modelCacheEntry           `json:"model"`
	BaseModel          *modelCacheEntry           `json:"base_model"`
	Components         map[string]modelCacheEntry `json:"components"`
	Lora               any                        `json:"lora"`
	DType              string                     `json:"dtype"`
	QuantizationConfig json.RawMessage            `json:"quantization_config"`
}

// parsedModelCacheConfig holds the executable fields of a model cache config.
// Components and Lora use the Image TaskArgs shape.
type parsedModelCacheConfig struct {
	TaskKind           string
	ModelID            string
	DType              string
	QuantizationConfig json.RawMessage
	QuantizationKey    string
	Components         map[string]any
	ComponentsKey      string
	Lora               map[string]any
	LoraKey            string
}

// normalizeModelCacheHash validates a hex encoded 32-byte hash and returns its lowercase form.
func normalizeModelCacheHash(raw string) (string, error) {
	hash := strings.ToLower(strings.TrimSpace(raw))
	decoded, err := hex.DecodeString(hash)
	if err != nil {
		return "", fmt.Errorf("model_cache_hash is not hex: %w", err)
	}
	if len(decoded) != modelCacheHashBytes {
		return "", fmt.Errorf("model_cache_hash must be %d bytes, got %d", modelCacheHashBytes, len(decoded))
	}
	return hash, nil
}

// normalizeRepository lowercases Hugging Face repository names and keeps URLs unchanged.
func normalizeRepository(name string) string {
	if strings.HasPrefix(name, "http://") || strings.HasPrefix(name, "https://") {
		return name
	}
	return strings.ToLower(name)
}

func normalizeExecutionDType(dtype string) string {
	dtype = strings.ToLower(strings.TrimSpace(dtype))
	if dtype == "" {
		return autoExecutionDType
	}
	return dtype
}

// normalizeComponentSource converts a component source to its identity shape:
// format, normalized repo, and format_kwargs when present. The revision is not part of the identity.
func normalizeComponentSource(usage string, format, repo string, formatKwargs map[string]any) (map[string]any, error) {
	if usage == "" || format == "" || repo == "" {
		return nil, fmt.Errorf("component %q requires usage name, format and repo", usage)
	}
	source := map[string]any{
		"format": format,
		"repo":   normalizeRepository(repo),
	}
	if formatKwargs != nil {
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

func (e *modelCacheEntry) repository() string {
	if e == nil {
		return ""
	}
	return normalizeRepository(e.Repository)
}

// parseModelCacheConfig parses the canonical Relay model cache config into executable fields.
func parseModelCacheConfig(raw json.RawMessage) (parsedModelCacheConfig, error) {
	var config modelCacheConfig
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&config); err != nil {
		return parsedModelCacheConfig{}, fmt.Errorf("invalid model_cache_config: %w", err)
	}
	quantRaw, quantKey, err := normalizeQuantizationConfig(config.QuantizationConfig)
	if err != nil {
		return parsedModelCacheConfig{}, fmt.Errorf("invalid model_cache_config quantization_config: %w", err)
	}
	parsed := parsedModelCacheConfig{
		TaskKind:           config.TaskKind,
		DType:              strings.ToLower(strings.TrimSpace(config.DType)),
		QuantizationConfig: quantRaw,
		QuantizationKey:    quantKey,
	}
	switch config.TaskKind {
	case modelCacheTaskKindLLM:
		parsed.ModelID = config.Model.repository()
	case modelCacheTaskKindImage:
		parsed.ModelID = config.BaseModel.repository()
		components := make(map[string]any, len(config.Components))
		for usage, entry := range config.Components {
			source := map[string]any{
				"format": entry.Format,
				"repo":   entry.Repository,
			}
			if entry.FormatKwargs != nil {
				source["format_kwargs"] = entry.FormatKwargs
			}
			components[usage] = source
		}
		if parsed.Components, parsed.ComponentsKey, err = normalizeTaskArgsComponents(components); err != nil {
			return parsedModelCacheConfig{}, fmt.Errorf("invalid model_cache_config components: %w", err)
		}
		if parsed.Lora, parsed.LoraKey, err = normalizeTaskArgsLora(config.Lora); err != nil {
			return parsedModelCacheConfig{}, fmt.Errorf("invalid model_cache_config lora: %w", err)
		}
	default:
		return parsedModelCacheConfig{}, fmt.Errorf("unsupported model_cache_config task_kind %q", config.TaskKind)
	}
	if parsed.ModelID == "" {
		return parsedModelCacheConfig{}, errors.New("model_cache_config model is empty")
	}
	return parsed, nil
}
