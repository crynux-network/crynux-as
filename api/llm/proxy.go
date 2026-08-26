package llm

import (
	"bufio"
	"bytes"
	"context"
	"crynux_as/bridge"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type usagePayload struct {
	PromptTokens     uint64 `json:"prompt_tokens"`
	CompletionTokens uint64 `json:"completion_tokens"`
	TotalTokens      uint64 `json:"total_tokens"`
}

type requestMeta struct {
	Model               string  `json:"model"`
	Stream              bool    `json:"stream"`
	MaxTokens           *int    `json:"max_tokens"`
	MaxCompletionTokens *int    `json:"max_completion_tokens"`
	VramLimit           *uint64 `json:"vram_limit"`
	StreamOptions       *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

var estimateTaskFeeFn = service.EstimateTaskFee

func handleLLMProxy(c *gin.Context, estimatePrompt func([]byte) uint64, forward func(context.Context, uint64, []byte) (*bridge.Response, error)) {
	project := GetProject(c)
	if project == nil {
		writeAuthError(c, "unauthorized")
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		writeClientError(c, http.StatusBadRequest, "failed to read request body")
		return
	}

	var meta requestMeta
	if err := json.Unmarshal(body, &meta); err != nil {
		writeClientError(c, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(meta.Model) == "" {
		writeClientError(c, http.StatusBadRequest, "model is required")
		return
	}

	userVram, err := resolveUserVramLimit(meta.VramLimit, c.Param("vram_limit"))
	if err != nil {
		writeClientError(c, http.StatusBadRequest, err.Error())
		return
	}
	effectiveVram := resolveEffectiveVram(meta.Model, userVram)

	appCfg := config.GetConfig()
	vramRatio := service.SelectVramRatio(appCfg.LLM.VramRatios, effectiveVram)
	prices := service.LLMPrices{
		PromptCreditsPerToken:     appCfg.LLM.PromptCreditsPerToken,
		CompletionCreditsPerToken: appCfg.LLM.CompletionCreditsPerToken,
	}

	db := config.GetDB()
	account, err := loadCreditAccount(c.Request.Context(), db, project.UserID)
	if err != nil {
		log.Errorf("Error loading credit account for user %d: %v", project.UserID, err)
		writeServerError(c)
		return
	}

	estPrompt := estimatePrompt(body)
	maxCompletion := service.ResolveMaxCompletionTokens(meta.MaxTokens, meta.MaxCompletionTokens, appCfg.LLM.DefaultMaxTokens)
	estimated := service.CalcCredits(estPrompt, maxCompletion, project.TokenRatio, vramRatio, prices)
	if err := service.EnsureSufficientBalance(&account.Balance.Int, estimated); err != nil {
		writeClientError(c, http.StatusPaymentRequired, "insufficient credits balance")
		return
	}

	started := time.Now()
	taskFee, err := estimateTaskFeeFn(
		c.Request.Context(),
		meta.Model,
		effectiveVram,
		project.TokenRatio,
		estPrompt,
		maxCompletion,
	)
	if err != nil {
		log.Errorf("Task fee estimation failed for project %d model %s: %v", project.ID, meta.Model, err)
		_ = recordFailedCall(c.Request.Context(), project, meta.Model, effectiveVram, time.Since(started), nil)
		writeServerError(c)
		return
	}

	clientRequestedIncludeUsage := meta.Stream && meta.StreamOptions != nil && meta.StreamOptions.IncludeUsage
	forwardBody := body
	if meta.Stream {
		injected, err := injectStreamIncludeUsage(body)
		if err != nil {
			writeClientError(c, http.StatusBadRequest, "invalid JSON body")
			return
		}
		forwardBody = injected
	}

	bridgeResp, err := forward(c.Request.Context(), effectiveVram, forwardBody)
	if err != nil {
		log.Errorf("Bridge request failed for project %d: %v", project.ID, err)
		_ = recordFailedCall(c.Request.Context(), project, meta.Model, effectiveVram, time.Since(started), nil)
		writeServerError(c)
		return
	}

	if bridgeResp.StatusCode >= 400 {
		respBody, readErr := bridgeResp.ReadAll()
		duration := time.Since(started)
		_ = recordFailedCall(c.Request.Context(), project, meta.Model, effectiveVram, duration, nil)
		if readErr != nil {
			log.Errorf("Error reading bridge error body for project %d: %v", project.ID, readErr)
			writeServerError(c)
			return
		}
		contentType := bridgeResp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		c.Data(bridgeResp.StatusCode, contentType, respBody)
		return
	}

	if meta.Stream {
		proxyStreamResponse(c, bridgeResp, project, meta.Model, effectiveVram, vramRatio, clientRequestedIncludeUsage, prices, started, taskFee)
		return
	}

	proxyJSONResponse(c, bridgeResp, project, meta.Model, effectiveVram, vramRatio, prices, started, taskFee)
}

func proxyJSONResponse(c *gin.Context, bridgeResp *bridge.Response, project *models.Project, model string, billedVram uint64, vramRatio uint, prices service.LLMPrices, started time.Time, taskFee *service.CalcTaskFeeResult) {
	respBody, err := bridgeResp.ReadAll()
	duration := time.Since(started)
	if err != nil {
		log.Errorf("Error reading bridge response for project %d: %v", project.ID, err)
		_ = recordFailedCall(c.Request.Context(), project, model, billedVram, duration, nil)
		writeServerError(c)
		return
	}

	var parsed struct {
		Usage usagePayload `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		log.Errorf("Error parsing bridge usage for project %d: %v", project.ID, err)
		_ = recordFailedCall(c.Request.Context(), project, model, billedVram, duration, nil)
		writeServerError(c)
		return
	}

	credits := service.CalcCredits(parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens, project.TokenRatio, vramRatio, prices)
	if err := service.ProcessLLMCall(c.Request.Context(), config.GetDB(), buildSuccessCallInput(
		project, model, parsed.Usage, credits, duration, billedVram, taskFee,
	)); err != nil {
		log.Errorf("Error recording LLM call for project %d: %v", project.ID, err)
		writeServerError(c)
		return
	}

	contentType := bridgeResp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(http.StatusOK, contentType, respBody)
}

func proxyStreamResponse(
	c *gin.Context,
	bridgeResp *bridge.Response,
	project *models.Project,
	model string,
	billedVram uint64,
	vramRatio uint,
	clientRequestedIncludeUsage bool,
	prices service.LLMPrices,
	started time.Time,
	taskFee *service.CalcTaskFeeResult,
) {
	defer bridgeResp.Close()

	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		log.Errorf("ResponseWriter does not support flushing for project %d", project.ID)
		_ = recordFailedCall(c.Request.Context(), project, model, billedVram, time.Since(started), nil)
		writeServerError(c)
		return
	}

	var usage *usagePayload
	reader := bufio.NewReader(bridgeResp.Body)
	streamErr := false

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := bytes.TrimRight(line, "\r\n")
			payload := bytes.TrimSpace(trimmed)

			if bytes.HasPrefix(payload, []byte("data:")) {
				data := bytes.TrimSpace(bytes.TrimPrefix(payload, []byte("data:")))
				if bytes.Equal(data, []byte("[DONE]")) {
					if _, writeErr := c.Writer.Write([]byte("data: [DONE]\n\n")); writeErr != nil {
						streamErr = true
						break
					}
					flusher.Flush()
					break
				}

				var chunk map[string]json.RawMessage
				if json.Unmarshal(data, &chunk) == nil {
					if rawUsage, hasUsage := chunk["usage"]; hasUsage && string(rawUsage) != "null" {
						var u usagePayload
						if json.Unmarshal(rawUsage, &u) == nil {
							usage = &u
						}
					}
					if !clientRequestedIncludeUsage && isUsageOnlyChunk(chunk) {
						continue
					}
				}
			}

			if _, writeErr := c.Writer.Write(line); writeErr != nil {
				streamErr = true
				break
			}
			if bytes.HasSuffix(line, []byte("\n")) {
				flusher.Flush()
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			log.Errorf("Error reading bridge stream for project %d: %v", project.ID, err)
			streamErr = true
			break
		}
	}

	duration := time.Since(started)
	if streamErr || usage == nil {
		_ = recordFailedCall(c.Request.Context(), project, model, billedVram, duration, nil)
		return
	}

	credits := service.CalcCredits(usage.PromptTokens, usage.CompletionTokens, project.TokenRatio, vramRatio, prices)
	if err := service.ProcessLLMCall(c.Request.Context(), config.GetDB(), buildSuccessCallInput(
		project, model, *usage, credits, duration, billedVram, taskFee,
	)); err != nil {
		log.Errorf("Error recording streamed LLM call for project %d: %v", project.ID, err)
	}
}

func buildSuccessCallInput(
	project *models.Project,
	model string,
	usage usagePayload,
	credits *big.Int,
	duration time.Duration,
	billedVram uint64,
	taskFee *service.CalcTaskFeeResult,
) service.RecordLLMCallInput {
	in := service.RecordLLMCallInput{
		UserID:           project.UserID,
		ProjectID:        project.ID,
		Model:            model,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		TokenRatio:       project.TokenRatio,
		Status:           models.LLMCallStatusSuccess,
		Credits:          credits,
		DurationMs:       uint64(duration.Milliseconds()),
		BilledVram:       billedVram,
		Charge:           true,
	}
	if taskFee != nil {
		in.TaskFeeGwei = taskFee.TaskFeeGwei
		in.MedianPriorityGwei = taskFee.MedianPriorityGwei
		estimated := taskFee.EstimatedNodeSeconds
		weight := taskFee.VramWeight
		in.EstimatedNodeSeconds = &estimated
		in.VramWeight = &weight
	}
	return in
}

func isUsageOnlyChunk(chunk map[string]json.RawMessage) bool {
	rawUsage, hasUsage := chunk["usage"]
	if !hasUsage || string(rawUsage) == "null" {
		return false
	}
	rawChoices, hasChoices := chunk["choices"]
	if !hasChoices {
		return true
	}
	var choices []json.RawMessage
	if err := json.Unmarshal(rawChoices, &choices); err != nil {
		return false
	}
	return len(choices) == 0
}

func injectStreamIncludeUsage(body []byte) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, err
	}
	obj["stream_options"] = json.RawMessage(`{"include_usage":true}`)
	return json.Marshal(obj)
}

func loadCreditAccount(ctx context.Context, db *gorm.DB, userID uint) (*models.CreditAccount, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var account models.CreditAccount
	if err := db.WithContext(dbCtx).Where("user_id = ?", userID).First(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func recordFailedCall(ctx context.Context, project *models.Project, model string, billedVram uint64, duration time.Duration, taskFee *service.CalcTaskFeeResult) error {
	in := service.RecordLLMCallInput{
		UserID:     project.UserID,
		ProjectID:  project.ID,
		Model:      model,
		TokenRatio: project.TokenRatio,
		Status:     models.LLMCallStatusFailed,
		Credits:    big.NewInt(0),
		DurationMs: uint64(duration.Milliseconds()),
		BilledVram: billedVram,
		Charge:     false,
	}
	if taskFee != nil {
		in.TaskFeeGwei = taskFee.TaskFeeGwei
		in.MedianPriorityGwei = taskFee.MedianPriorityGwei
		estimated := taskFee.EstimatedNodeSeconds
		weight := taskFee.VramWeight
		in.EstimatedNodeSeconds = &estimated
		in.VramWeight = &weight
	}
	return service.ProcessLLMCall(ctx, config.GetDB(), in)
}

func writeClientError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}

func writeServerError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": gin.H{
			"message": "internal server error",
			"type":    "server_error",
		},
	})
}
