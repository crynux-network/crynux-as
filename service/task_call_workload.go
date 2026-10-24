package service

import (
	"crynux_as/llmadapter"
	"crynux_as/models"
	"encoding/json"
	"strings"
)

// TaskCallWorkloadFields are Relay identity and Image workload fields stored on
// task_call_records and returned by Recent Requests / Charges.
type TaskCallWorkloadFields struct {
	ModelCacheHash     string
	ExecutionDType     string
	QuantizationConfig json.RawMessage
	Components         json.RawMessage
	ImageWidth         uint64
	ImageHeight        uint64
	ImageSteps         uint64
	ImageCount         uint64
}

// ExtractTaskCallWorkloadFields derives display and identity fields from TaskArgs.
// When a loaded-models exact match exists, model_cache_hash and execution_dtype
// come from that Relay view; otherwise hash stays empty and TaskArgs values are kept.
func ExtractTaskCallWorkloadFields(taskType models.TaskType, model string, taskArgsJSON string) TaskCallWorkloadFields {
	out := TaskCallWorkloadFields{}
	if strings.TrimSpace(taskArgsJSON) == "" {
		return out
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(taskArgsJSON), &payload); err != nil {
		return out
	}

	if taskType == models.TaskTypeImage {
		return extractImageTaskCallWorkload(model, taskArgsJSON, payload)
	}
	return extractLLMTaskCallWorkload(model, payload)
}

func extractLLMTaskCallWorkload(model string, payload map[string]any) TaskCallWorkloadFields {
	out := TaskCallWorkloadFields{}
	modelID := llmadapter.NormalizeModelID(model)
	if modelID == "" {
		if raw, ok := payload["model"].(string); ok {
			modelID = llmadapter.NormalizeModelID(raw)
		}
	}

	if dtype, ok := payload["dtype"].(string); ok {
		out.ExecutionDType = normalizeExecutionDType(dtype)
	}
	if quantRaw, ok := payload["quantization_config"]; ok {
		out.QuantizationConfig = marshalJSONRaw(quantRaw)
	}

	hasQuant := payload["quantization_config"] != nil
	hasDtype := out.ExecutionDType != "" && out.ExecutionDType != autoExecutionDType
	var quant any
	if hasQuant {
		quant = payload["quantization_config"]
	}
	if loaded, ok := lookupExactOrMax(modelID, quant, out.ExecutionDType, hasQuant, hasDtype); ok {
		out.ModelCacheHash = loaded.ModelCacheHash
		out.ExecutionDType = loaded.ExecutionDType
		if len(out.QuantizationConfig) == 0 {
			out.QuantizationConfig = cloneRawOrNull(loaded.QuantizationConfig)
		}
	}
	return out
}

func extractImageTaskCallWorkload(model string, taskArgsJSON string, payload map[string]any) TaskCallWorkloadFields {
	out := TaskCallWorkloadFields{}
	modelID := llmadapter.NormalizeModelID(model)
	if modelID == "" {
		if base, ok := payload["base_model"].(map[string]any); ok {
			if name, ok := base["name"].(string); ok {
				modelID = llmadapter.NormalizeModelID(name)
			}
		}
	}

	if dtype, ok := payload["dtype"].(string); ok {
		out.ExecutionDType = normalizeExecutionDType(dtype)
	}
	if quantRaw, ok := payload["quantization_config"]; ok {
		out.QuantizationConfig = marshalJSONRaw(quantRaw)
	}
	if componentsRaw, ok := payload["components"]; ok {
		if normalized, _, err := normalizeTaskArgsComponents(componentsRaw); err == nil {
			out.Components = marshalJSONRaw(normalized)
		} else {
			out.Components = marshalJSONRaw(componentsRaw)
		}
	}

	if cfg, ok := payload["task_config"].(map[string]any); ok {
		out.ImageWidth = uintFromAny(cfg["image_width"])
		out.ImageHeight = uintFromAny(cfg["image_height"])
		out.ImageSteps = uintFromAny(cfg["steps"])
	}
	if count, err := ImageCountFromTaskArgs(taskArgsJSON); err == nil {
		out.ImageCount = count
	}

	if loaded, ok, err := GetLoadedSDModelExact(modelID, taskArgsJSON); err == nil && ok {
		out.ModelCacheHash = loaded.ModelCacheHash
		out.ExecutionDType = loaded.ExecutionDType
		if len(out.QuantizationConfig) == 0 {
			out.QuantizationConfig = cloneRawOrNull(loaded.QuantizationConfig)
		}
		if len(out.Components) == 0 && loaded.Components != nil {
			out.Components = marshalJSONRaw(loaded.Components)
		}
	}
	return out
}

func uintFromAny(v any) uint64 {
	switch n := v.(type) {
	case float64:
		if n > 0 {
			return uint64(n)
		}
	case json.Number:
		u, err := n.Int64()
		if err == nil && u > 0 {
			return uint64(u)
		}
	case int:
		if n > 0 {
			return uint64(n)
		}
	case int64:
		if n > 0 {
			return uint64(n)
		}
	case uint64:
		return n
	}
	return 0
}

func marshalJSONRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return json.RawMessage(b)
}

func cloneRawOrNull(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	out := make(json.RawMessage, len(raw))
	copy(out, raw)
	return out
}

// TaskTypeAPIString maps stored TaskType to API string values.
func TaskTypeAPIString(taskType models.TaskType) string {
	if taskType == models.TaskTypeImage {
		return "image"
	}
	return "llm"
}
