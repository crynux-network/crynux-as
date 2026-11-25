package service

import (
	"crynux_as/llmadapter"
	"crynux_as/models"
	"encoding/json"
	"strings"

	log "github.com/sirupsen/logrus"
)

// TaskCallWorkloadFields are Relay identity and Image/Video workload fields
// stored on task_call_records and returned by Recent Requests / Charges.
type TaskCallWorkloadFields struct {
	ModelCacheHash     string
	DType              string
	QuantizationConfig json.RawMessage
	Components         json.RawMessage
	ImageWidth         uint64
	ImageHeight        uint64
	ImageSteps         uint64
	ImageCount         uint64
	VideoWidth         uint64
	VideoHeight        uint64
	VideoNumFrames     uint64
	VideoSteps         uint64
	VideoCount         uint64
}

// ExtractTaskCallWorkloadFields derives display and identity fields from resolved
// TaskArgs. model_cache_hash is the local deterministic hash of the TaskArgs
// execution identity and stays empty when the TaskArgs has no concrete dtype.
func ExtractTaskCallWorkloadFields(taskType models.TaskType, model string, taskArgsJSON string) TaskCallWorkloadFields {
	out := TaskCallWorkloadFields{}
	if strings.TrimSpace(taskArgsJSON) == "" {
		return out
	}
	payload, err := decodeJSONObjectUseNumber([]byte(taskArgsJSON))
	if err != nil {
		return out
	}

	switch taskType {
	case models.TaskTypeImage:
		return extractImageTaskCallWorkload(model, taskArgsJSON, payload)
	case models.TaskTypeVideo:
		return extractVideoTaskCallWorkload(model, taskArgsJSON, payload)
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

	if quantRaw, ok := payload["quantization_config"]; ok {
		out.QuantizationConfig = marshalJSONRaw(quantRaw)
	}
	out.DType, out.ModelCacheHash = workloadModelCacheHash(modelCacheTaskKindLLM, modelID, payload)
	return out
}

// workloadModelCacheHash returns empty values for TaskArgs without a dtype,
// such as Provider tasks.
func workloadModelCacheHash(taskKind, modelID string, payload map[string]any) (string, string) {
	if _, ok := payload["dtype"]; !ok {
		return "", ""
	}
	dtype, hash, err := taskArgsModelCacheHash(taskKind, modelID, payload)
	if err != nil {
		log.Debugf("cannot derive model cache hash for %s model %q: %v", taskKind, modelID, err)
		return "", ""
	}
	return dtype, hash
}

// diffusionWorkloadIdentity fills the model id, quantization_config, and
// components shared by Image and Video TaskArgs.
func diffusionWorkloadIdentity(model string, payload map[string]any) (string, TaskCallWorkloadFields) {
	out := TaskCallWorkloadFields{}
	modelID := llmadapter.NormalizeModelID(model)
	if modelID == "" {
		if base, ok := payload["base_model"].(map[string]any); ok {
			if name, ok := base["name"].(string); ok {
				modelID = llmadapter.NormalizeModelID(name)
			}
		}
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
	return modelID, out
}

// extractVideoTaskCallWorkload records the billed video dimensions. One Video
// task produces one video.
func extractVideoTaskCallWorkload(model string, taskArgsJSON string, payload map[string]any) TaskCallWorkloadFields {
	modelID, out := diffusionWorkloadIdentity(model, payload)
	if dims, err := resolveVideoTaskDimensions(taskArgsJSON); err == nil {
		out.VideoWidth = dims.Width
		out.VideoHeight = dims.Height
		out.VideoNumFrames = dims.NumFrames
		out.VideoSteps = dims.Steps
	} else {
		log.Debugf("cannot resolve video dimensions for model %q: %v", modelID, err)
	}
	out.VideoCount = 1
	out.DType, out.ModelCacheHash = workloadModelCacheHash(modelCacheTaskKindVideo, modelID, payload)
	return out
}

func extractImageTaskCallWorkload(model string, taskArgsJSON string, payload map[string]any) TaskCallWorkloadFields {
	modelID, out := diffusionWorkloadIdentity(model, payload)

	if cfg, ok := payload["task_config"].(map[string]any); ok {
		out.ImageWidth = uintFromAny(cfg["image_width"])
		out.ImageHeight = uintFromAny(cfg["image_height"])
		out.ImageSteps = uintFromAny(cfg["steps"])
	}
	if count, err := ImageCountFromTaskArgs(taskArgsJSON); err == nil {
		out.ImageCount = count
	}

	out.DType, out.ModelCacheHash = workloadModelCacheHash(modelCacheTaskKindImage, modelID, payload)
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
	switch taskType {
	case models.TaskTypeImage:
		return "image"
	case models.TaskTypeVideo:
		return "video"
	}
	return "llm"
}

// UintPtrIfPositive maps an unset (zero) workload field to API null.
func UintPtrIfPositive(v uint64) *uint64 {
	if v == 0 {
		return nil
	}
	return &v
}
