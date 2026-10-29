package llm

import (
	"bytes"
	"crynux_as/api/llm/responses"
	"crynux_as/llmadapter"
	"crynux_as/models"
	"crynux_as/service"
	"encoding/json"
	"io"
	"strings"
	"time"

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
	acceptedAt := time.Now()

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		_ = recordFailedCall(c.Request.Context(), project, models.TaskAPITypeResponses, "", 0, acceptedAt, time.Now(), nil, nil)
		writeClientError(c, 400, "failed to read request body")
		return
	}

	model := strings.TrimSpace(extractModelFromBody(body))
	if route, ok := service.LookupProviderModel(model); ok {
		parsed, err := parseProviderResponsesRequest(body, route.ModelName)
		if err != nil {
			_ = recordFailedCall(c.Request.Context(), project, models.TaskAPITypeResponses, route.ModelName, 0, acceptedAt, time.Now(), nil, nil)
			writeLLMAdapterError(c, err)
			return
		}
		handleProviderTaskJobRequest(c, project, models.TaskAPITypeResponses, body, parsed, route, acceptedAt)
		return
	}

	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	responses.CreateResponse(c)
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
