package service

import (
	"crynux_as/config"
	"crynux_as/llmadapter"
	"encoding/json"
	"fmt"
)

const modelNotReadyMessageTemplate = "This model is not yet available on any node in the network. The network has notified nodes to start downloading it. Downloads take some time, so this task cannot complete on time. Please try again in %d minutes."

// ModelNotReadyRetryMinutes returns the loaded-models refresh interval as whole minutes.
// Non-divisible intervals round up so the message never shows zero minutes.
func ModelNotReadyRetryMinutes() uint64 {
	seconds := config.GetConfig().LLM.LoadedModelsRefreshInterval
	if seconds == 0 {
		return 1
	}
	minutes := seconds / 60
	if seconds%60 != 0 {
		minutes++
	}
	if minutes == 0 {
		return 1
	}
	return minutes
}

// ModelNotReadyMessage returns the create-time HTTP 400 message for models with no on-disk nodes.
func ModelNotReadyMessage() string {
	return fmt.Sprintf(modelNotReadyMessageTemplate, ModelNotReadyRetryMinutes())
}

// CrynuxLLMModelReady reports whether any full-cache LLM row for modelID has on_disk_node_count > 0.
// Quantization and execution dtype are ignored: the same HF files cover runtime quant variants.
func CrynuxLLMModelReady(modelID string) bool {
	modelID = llmadapter.NormalizeModelID(modelID)
	if modelID == "" {
		return false
	}
	return llmModelHasOnDiskNodes(modelID)
}

// CrynuxImageTaskArgsReady reports whether any full-cache Image row matches the TaskArgs
// base model and components set with on_disk_node_count > 0. Quantization, dtype, and
// lora are ignored. Components identity MUST match as one set (Relay file-set semantics).
func CrynuxImageTaskArgsReady(taskArgsJSON string) bool {
	modelID, componentsKey, ok := diffusionInventoryIdentity(taskArgsJSON)
	if !ok {
		return false
	}
	return sdModelHasOnDiskNodes(modelID, componentsKey)
}

// CrynuxVideoTaskArgsReady reports whether any full-cache Video row matches the TaskArgs
// base model and components set with on_disk_node_count > 0. Quantization, dtype, and
// loras are ignored. Components identity MUST match as one set (Relay file-set semantics).
func CrynuxVideoTaskArgsReady(taskArgsJSON string) bool {
	modelID, componentsKey, ok := diffusionInventoryIdentity(taskArgsJSON)
	if !ok {
		return false
	}
	return videoModelHasOnDiskNodes(modelID, componentsKey)
}

func diffusionInventoryIdentity(taskArgsJSON string) (modelID string, componentsKey string, ok bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(taskArgsJSON), &payload); err != nil {
		return "", "", false
	}
	baseRaw, present := payload["base_model"]
	if !present || baseRaw == nil {
		return "", "", false
	}
	base, isObject := baseRaw.(map[string]any)
	if !isObject {
		return "", "", false
	}
	name, _ := base["name"].(string)
	modelID = llmadapter.NormalizeModelID(name)
	if modelID == "" {
		return "", "", false
	}
	_, componentsKey, err := normalizeTaskArgsComponents(payload["components"])
	if err != nil {
		return "", "", false
	}
	return modelID, componentsKey, true
}
