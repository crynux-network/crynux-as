package service

import (
	"crynux_as/llmadapter"
	"crynux_as/models"
	"encoding/json"
	"fmt"
	"strings"
)

// NormalizeTaskArgsModelNames rewrites Hugging Face model name fields inside
// TaskArgs JSON with llmadapter.NormalizeModelName so persisted TaskArgs, job
// model, Bridge submit, and loaded-models matching use the same casing.
func NormalizeTaskArgsModelNames(taskArgs string, taskType models.TaskType) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(taskArgs))
	decoder.UseNumber()
	var argsMap map[string]any
	if err := decoder.Decode(&argsMap); err != nil {
		return "", fmt.Errorf("task_args must be valid JSON: %w", err)
	}
	if argsMap == nil {
		return "", fmt.Errorf("task_args must be a JSON object")
	}

	switch taskType {
	case models.TaskTypeImage:
		if baseModel, ok := argsMap["base_model"].(string); ok {
			argsMap["base_model"] = llmadapter.NormalizeModelName(baseModel)
		} else {
			normalizeObjectModelName(argsMap, "base_model", "name")
		}
		normalizeObjectModelName(argsMap, "lora", "model")
		normalizeObjectModelName(argsMap, "controlnet", "model")
		normalizeObjectModelName(argsMap, "refiner", "model")
		normalizeStringModelName(argsMap, "unet")
		normalizeStringModelName(argsMap, "vae")
		normalizeStringModelName(argsMap, "textual_inversion")
		normalizeComponentsRepos(argsMap)
	case models.TaskTypeLLM:
		if model, ok := argsMap["model"].(string); ok {
			argsMap["model"] = llmadapter.NormalizeModelName(model)
		}
	default:
		return "", fmt.Errorf("task_type must be 0 or 1")
	}

	normalized, err := json.Marshal(argsMap)
	if err != nil {
		return "", err
	}
	return string(normalized), nil
}

func normalizeStringModelName(argsMap map[string]any, key string) {
	if name, ok := argsMap[key].(string); ok {
		argsMap[key] = llmadapter.NormalizeModelName(name)
	}
}

func normalizeObjectModelName(argsMap map[string]any, objectKey, nameKey string) {
	object, ok := argsMap[objectKey].(map[string]any)
	if !ok {
		return
	}
	if name, ok := object[nameKey].(string); ok {
		object[nameKey] = llmadapter.NormalizeModelName(name)
	}
}

func normalizeComponentsRepos(argsMap map[string]any) {
	components, ok := argsMap["components"].(map[string]any)
	if !ok {
		return
	}
	for _, raw := range components {
		object, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if repo, ok := object["repo"].(string); ok {
			object["repo"] = llmadapter.NormalizeModelName(repo)
		}
	}
}
