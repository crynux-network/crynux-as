package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

var bitsAndBytesQuantKwargsKeys = map[string]struct{}{
	"load_in_8bit":                     {},
	"load_in_4bit":                     {},
	"llm_int8_threshold":               {},
	"llm_int8_skip_modules":            {},
	"llm_int8_enable_fp32_cpu_offload": {},
	"llm_int8_has_fp16_weight":         {},
	"bnb_4bit_compute_dtype":           {},
	"bnb_4bit_quant_type":              {},
	"bnb_4bit_use_double_quant":        {},
	"bnb_4bit_quant_storage":           {},
}

var bnb4BitComputeDTypes = map[string]struct{}{
	"float16":  {},
	"bfloat16": {},
	"float32":  {},
}

var bnb4BitQuantTypes = map[string]struct{}{
	"nf4": {},
	"fp4": {},
}

var bnb4BitQuantStorages = map[string]struct{}{
	"float16":  {},
	"float32":  {},
	"int8":     {},
	"uint8":    {},
	"float64":  {},
	"bfloat16": {},
}

// ValidateTaskArgsQuantizationCompat rejects quantization_config values that
// workers reject at BitsAndBytesConfig construction time. It runs on the final
// TaskArgs JSON before Credits reservation.
func ValidateTaskArgsQuantizationCompat(taskArgsJSON string, taskType TaskType) error {
	decoder := json.NewDecoder(bytes.NewReader([]byte(taskArgsJSON)))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return fmt.Errorf("task_args must be valid JSON: %w", err)
	}
	if payload == nil {
		return fmt.Errorf("task_args must be a JSON object")
	}

	rawQuant, ok := payload["quantization_config"]
	if !ok || rawQuant == nil {
		return nil
	}
	quant, ok := rawQuant.(map[string]any)
	if !ok {
		return fmt.Errorf("quantization_config must be an object or null")
	}

	methodRaw, ok := quant["quant_method"]
	if !ok {
		return fmt.Errorf("quantization_config.quant_method is required")
	}
	method, ok := methodRaw.(string)
	if !ok || strings.TrimSpace(method) == "" {
		return fmt.Errorf("quantization_config.quant_method must be a non-empty string")
	}
	method = strings.TrimSpace(method)

	switch taskType {
	case TaskTypeImage, TaskTypeVideo:
		if method != "bitsandbytes" {
			return fmt.Errorf("quantization_config.quant_method must be bitsandbytes for image and video tasks")
		}
	case TaskTypeLLM:
		if method != "bitsandbytes" && method != "fp8" {
			return fmt.Errorf("quantization_config.quant_method must be bitsandbytes or fp8 for LLM tasks")
		}
	default:
		return nil
	}

	if method == "fp8" {
		return nil
	}

	kwargsRaw, ok := quant["quant_kwargs"]
	if !ok || kwargsRaw == nil {
		return fmt.Errorf("quantization_config.quant_kwargs is required when quant_method is bitsandbytes")
	}
	kwargs, ok := kwargsRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("quantization_config.quant_kwargs must be an object")
	}
	return validateBitsAndBytesQuantKwargs(kwargs)
}

func validateBitsAndBytesQuantKwargs(kwargs map[string]any) error {
	for key := range kwargs {
		if _, ok := bitsAndBytesQuantKwargsKeys[key]; !ok {
			return fmt.Errorf("quantization_config.quant_kwargs contains unsupported field %s", key)
		}
	}

	loadIn4Bit, has4Bit, err := optionalBool(kwargs, "load_in_4bit")
	if err != nil {
		return err
	}
	loadIn8Bit, has8Bit, err := optionalBool(kwargs, "load_in_8bit")
	if err != nil {
		return err
	}
	four := has4Bit && loadIn4Bit
	eight := has8Bit && loadIn8Bit
	if four == eight {
		return fmt.Errorf("quantization_config.quant_kwargs must set exactly one of load_in_4bit or load_in_8bit when quant_method is bitsandbytes")
	}

	if raw, ok := kwargs["llm_int8_threshold"]; ok {
		if err := validateLLMInt8Threshold(raw); err != nil {
			return err
		}
	}
	if raw, ok := kwargs["llm_int8_skip_modules"]; ok {
		if err := validateLLMInt8SkipModules(raw); err != nil {
			return err
		}
	}
	if err := requireFalseOrAbsent(kwargs, "llm_int8_enable_fp32_cpu_offload"); err != nil {
		return err
	}
	if err := requireFalseOrAbsent(kwargs, "llm_int8_has_fp16_weight"); err != nil {
		return err
	}
	if err := optionalStringInSet(kwargs, "bnb_4bit_quant_type", bnb4BitQuantTypes); err != nil {
		return err
	}
	if err := optionalStringInSet(kwargs, "bnb_4bit_compute_dtype", bnb4BitComputeDTypes); err != nil {
		return err
	}
	if _, _, err := optionalBool(kwargs, "bnb_4bit_use_double_quant"); err != nil {
		return err
	}
	if err := optionalStringInSet(kwargs, "bnb_4bit_quant_storage", bnb4BitQuantStorages); err != nil {
		return err
	}
	return nil
}

func validateLLMInt8Threshold(raw any) error {
	if raw == nil {
		return fmt.Errorf("quantization_config.quant_kwargs.llm_int8_threshold must be a JSON number with a fractional part (for example 6.0)")
	}
	number, ok := raw.(json.Number)
	if !ok {
		return fmt.Errorf("quantization_config.quant_kwargs.llm_int8_threshold must be a JSON number with a fractional part (for example 6.0)")
	}
	token := number.String()
	if !strings.ContainsAny(token, ".eE") {
		return fmt.Errorf("quantization_config.quant_kwargs.llm_int8_threshold must be a JSON number with a fractional part (for example 6.0)")
	}
	if _, err := number.Float64(); err != nil {
		return fmt.Errorf("quantization_config.quant_kwargs.llm_int8_threshold must be a JSON number with a fractional part (for example 6.0)")
	}
	return nil
}

func validateLLMInt8SkipModules(raw any) error {
	if raw == nil {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("quantization_config.quant_kwargs.llm_int8_skip_modules must be null or an array of strings")
	}
	for _, item := range list {
		if _, ok := item.(string); !ok {
			return fmt.Errorf("quantization_config.quant_kwargs.llm_int8_skip_modules must be null or an array of strings")
		}
	}
	return nil
}

func optionalBool(kwargs map[string]any, key string) (value bool, present bool, err error) {
	raw, ok := kwargs[key]
	if !ok {
		return false, false, nil
	}
	b, ok := raw.(bool)
	if !ok {
		return false, false, fmt.Errorf("quantization_config.quant_kwargs.%s must be a boolean", key)
	}
	return b, true, nil
}

func requireFalseOrAbsent(kwargs map[string]any, key string) error {
	raw, ok := kwargs[key]
	if !ok {
		return nil
	}
	b, ok := raw.(bool)
	if !ok {
		return fmt.Errorf("quantization_config.quant_kwargs.%s must be a boolean", key)
	}
	if b {
		return fmt.Errorf("quantization_config.quant_kwargs.%s must be false", key)
	}
	return nil
}

func optionalStringInSet(kwargs map[string]any, key string, allowed map[string]struct{}) error {
	raw, ok := kwargs[key]
	if !ok {
		return nil
	}
	s, ok := raw.(string)
	if !ok {
		return fmt.Errorf("quantization_config.quant_kwargs.%s must be a string", key)
	}
	if _, ok := allowed[s]; !ok {
		return fmt.Errorf("quantization_config.quant_kwargs.%s has an unsupported value", key)
	}
	return nil
}
