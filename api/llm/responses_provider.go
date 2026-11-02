package llm

import (
	"bytes"
	"crynux_as/api/llm/responses"
	"crynux_as/config"
	"crynux_as/llmadapter"
	"crynux_as/models"
	"crynux_as/service"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
)

// CreateResponse routes Provider models through the Provider job path and
// keeps Crynux Bridge Responses behavior unchanged for non-Provider models.
func CreateResponse(c *gin.Context) {
	project := GetProject(c)
	if project == nil {
		writeAuthError(c, "unauthorized")
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		writeRequestBodyReadError(c, err)
		return
	}

	model := strings.TrimSpace(extractModelFromBody(body))
	if route, ok := service.LookupProviderModel(model); ok {
		expandedBody, err := expandProviderResponsesBody(c, project, body, route.ModelName)
		if err != nil {
			return
		}
		parsed, err := parseProviderResponsesRequest(expandedBody, route.ModelName)
		if err != nil {
			writeLLMAdapterError(c, err)
			return
		}
		handleProviderTaskJobRequest(c, project, models.TaskAPITypeResponses, expandedBody, parsed, route)
		return
	}

	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	responses.CreateResponse(c)
}

func expandProviderResponsesBody(c *gin.Context, project *models.Project, body []byte, modelName string) ([]byte, error) {
	var probe struct {
		PreviousResponseID string `json:"previous_response_id"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		writeLLMAdapterError(c, &llmadapter.ValidationError{Message: "invalid JSON body"})
		return nil, err
	}
	if strings.TrimSpace(probe.PreviousResponseID) == "" {
		return stripPreviousResponseID(body), nil
	}

	appCfg := config.GetConfig()
	prevJob, err := service.LoadPreviousResponsesJob(
		c.Request.Context(),
		config.GetDB(),
		project.ID,
		probe.PreviousResponseID,
		appCfg.LLM.JobRetentionDays,
	)
	if err != nil {
		if errors.Is(err, service.ErrPreviousResponsesInvalid) {
			writeLLMAdapterError(c, llmadapter.NewValidationError("previous_response_id", "previous response is invalid"))
			return nil, err
		}
		writeLLMAdapterError(c, err)
		return nil, err
	}
	if prevJob.RawResultJSON == nil {
		err := llmadapter.NewValidationError("previous_response_id", "previous response is invalid")
		writeLLMAdapterError(c, err)
		return nil, err
	}
	history, err := llmadapter.BuildResponsesHistoryFromPreviousJob(prevJob.PublicID, prevJob.TaskArgsJSON, *prevJob.RawResultJSON)
	if err != nil {
		writeLLMAdapterError(c, err)
		return nil, err
	}
	expanded, err := llmadapter.ExpandProviderResponsesRequest(body, history)
	if err != nil {
		writeLLMAdapterError(c, err)
		return nil, err
	}
	return expanded, nil
}

func stripPreviousResponseID(body []byte) []byte {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	if _, ok := payload["previous_response_id"]; !ok {
		return body
	}
	delete(payload, "previous_response_id")
	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

func parseProviderResponsesRequest(body []byte, modelName string) (parsedTaskJobRequest, error) {
	var req struct {
		Model              string  `json:"model"`
		Stream             bool    `json:"stream"`
		Background         bool    `json:"background"`
		MaxOutputTokens    *int    `json:"max_output_tokens"`
		QueueTimeout       *uint64 `json:"queue_timeout"`
		VramLimit          *uint64 `json:"vram_limit"`
		Dtype              string  `json:"dtype"`
		QuantizationConfig any     `json:"quantization_config"`
		Input              any     `json:"input"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return parsedTaskJobRequest{}, &llmadapter.ValidationError{Message: "invalid JSON body"}
	}
	if strings.TrimSpace(req.Model) == "" {
		return parsedTaskJobRequest{}, &llmadapter.ValidationError{Field: "model", Message: "model is required"}
	}
	if req.Input == nil {
		return parsedTaskJobRequest{}, &llmadapter.ValidationError{Field: "input", Message: "input is required"}
	}
	if req.Stream && req.Background {
		return parsedTaskJobRequest{}, &llmadapter.ValidationError{Field: "stream", Message: "stream and background cannot both be true"}
	}
	return parsedTaskJobRequest{
		model:               modelName,
		stream:              req.Stream,
		background:          req.Background,
		vramLimit:           req.VramLimit,
		queueTimeout:        req.QueueTimeout,
		maxTokens:           req.MaxOutputTokens,
		maxCompletionTokens: req.MaxOutputTokens,
		taskArgsJSON:        string(body),
		streamIncludeUsage:  true,
		dtype:               req.Dtype,
		quantizationConfig:  req.QuantizationConfig,
	}, nil
}
