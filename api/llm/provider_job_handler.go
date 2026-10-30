package llm

import (
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/provider"
	"crynux_as/service"
	"crynux_as/utils"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

func handleProviderTaskJobRequest(c *gin.Context, project *models.Project, apiType models.TaskAPIType, body []byte, parsed parsedTaskJobRequest, route service.ProviderModelRoute, acceptedAt time.Time) {
	if apiType == models.TaskAPITypeCompletions {
		_ = recordFailedCall(c.Request.Context(), project, apiType, parsed.model, 0, acceptedAt, time.Now(), nil, nil)
		writeClientError(c, http.StatusBadRequest, "Provider models do not support the Completions API")
		return
	}
	if parsed.vramLimit != nil || parsed.dtype != "" || parsed.quantizationConfig != nil {
		_ = recordFailedCall(c.Request.Context(), project, apiType, parsed.model, 0, acceptedAt, time.Now(), nil, nil)
		writeClientError(c, http.StatusBadRequest, "Crynux execution fields are not supported for Provider models")
		return
	}
	if c.Param("vram_limit") != "" {
		_ = recordFailedCall(c.Request.Context(), project, apiType, parsed.model, 0, acceptedAt, time.Now(), nil, nil)
		writeClientError(c, http.StatusBadRequest, "vram_limit is not supported for Provider models")
		return
	}

	db := config.GetDB()
	appCfg := config.GetConfig()
	priorityGwei, err := service.ResolveProviderEffectivePriority(project)
	if err != nil {
		log.Errorf("resolve provider priority failed: %v", err)
		_ = recordFailedCall(c.Request.Context(), project, apiType, parsed.model, 0, acceptedAt, time.Now(), nil, nil)
		writeServerError(c)
		return
	}
	inputBytes := service.EstimateProviderInputBytes(body)
	maxCompletion := service.ResolveMaxCompletionTokens(parsed.maxTokens, parsed.maxCompletionTokens, appCfg.LLM.DefaultMaxTokens)
	snapshot, credits, err := service.BuildProviderBillingData(project, route, priorityGwei, inputBytes, maxCompletion, nil)
	if err != nil {
		_ = recordFailedCall(c.Request.Context(), project, apiType, parsed.model, 0, acceptedAt, time.Now(), priorityGwei, nil)
		if errors.Is(err, service.ErrTaskFeeBelowMinimum) || errors.Is(err, service.ErrAutoMaxCreditsInsufficient) {
			writeClientError(c, http.StatusBadRequest, err.Error())
			return
		}
		log.Errorf("provider billing estimate failed: %v", err)
		writeServerError(c)
		return
	}
	adjustedPriority, ok := new(big.Int).SetString(snapshot.EffectivePriorityGwei, 10)
	if !ok || adjustedPriority.Sign() <= 0 {
		log.Errorf("provider billing snapshot has invalid effective priority for project %d", project.ID)
		_ = recordFailedCall(c.Request.Context(), project, apiType, parsed.model, 0, acceptedAt, time.Now(), priorityGwei, nil)
		writeServerError(c)
		return
	}
	queueTimeout := service.ResolveProviderQueueTimeout(parsed.queueTimeout)
	publicID := ""
	if apiType == models.TaskAPITypeResponses {
		token, err := utils.GenerateRandomToken(16)
		if err != nil {
			writeServerError(c)
			return
		}
		publicID = "resp_" + token
	}

	job, err := service.ReserveCreditsAndCreateProviderTaskJob(c.Request.Context(), db, service.CreateProviderTaskJobInput{
		Project:         project,
		APIType:         apiType,
		Route:           route,
		Background:      parsed.background,
		Stream:          parsed.stream,
		RequestBody:     body,
		PriorityGwei:    adjustedPriority,
		CreditsToLock:   credits,
		QueueTimeout:    queueTimeout,
		BillingSnapshot: snapshot,
		PublicID:        publicID,
	})
	if err != nil {
		if errors.Is(err, service.ErrInsufficientBalance) {
			writePaymentRequired(c)
			return
		}
		log.Errorf("create provider job failed: %v", err)
		writeServerError(c)
		return
	}

	if parsed.stream {
		ch := service.GetProviderStreamRegistry().Register(job.ID)
		if err := db.Model(job).Update("phase", "queued").Error; err != nil {
			service.GetProviderStreamRegistry().Unregister(job.ID)
			log.Errorf("publish streaming provider job failed: %v", err)
			writeServerError(c)
			return
		}
		defer service.GetProviderStreamRegistry().Unregister(job.ID)
		writeProviderStream(c, ch, apiType, job)
		return
	}

	if parsed.background && apiType == models.TaskAPITypeResponses {
		c.JSON(http.StatusOK, gin.H{
			"id":     job.PublicID,
			"object": "response",
			"status": "queued",
			"model":  job.Model,
		})
		return
	}

	finished, err := service.WaitForTaskJob(c.Request.Context(), db, job.ID, time.Duration(queueTimeout+appCfg.LLM.JobSubmitTimeout)*time.Second)
	if err != nil && !errors.Is(err, service.ErrTaskJobWaitTimeout) {
		writeServerError(c)
		return
	}
	if finished == nil || !finished.IsTerminal() {
		writeTimeoutError(c)
		return
	}
	if finished.Status == models.TaskJobStatusFailed {
		msg := "provider job failed"
		if finished.ErrorMessage != nil {
			msg = *finished.ErrorMessage
		}
		writeClientError(c, http.StatusInternalServerError, msg)
		return
	}
	if finished.FormattedResultJSON == nil {
		writeServerError(c)
		return
	}
	var payload any
	if err := json.Unmarshal([]byte(*finished.FormattedResultJSON), &payload); err != nil {
		writeServerError(c)
		return
	}
	c.JSON(http.StatusOK, payload)
}

func writeProviderStream(c *gin.Context, ch <-chan service.ProviderStreamEvent, apiType models.TaskAPIType, job *models.TaskJob) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return
	}
	includeUsage := provider.RequestIncludeUsage([]byte(job.TaskArgsJSON), apiType)
	if apiType == models.TaskAPITypeResponses {
		includeUsage = true
	}
	encoder := provider.NewPublicStreamEncoder(
		apiType,
		provider.PublicStreamResponseID(apiType, job.ID, job.PublicID),
		job.Model,
		job.CreatedAt.Unix(),
		includeUsage,
	)
	for {
		select {
		case <-c.Request.Context().Done():
			service.GetProviderStreamRegistry().Detach(job.ID)
			return
		case event, open := <-ch:
			if !open {
				return
			}
			if event.Kind == "done" {
				if apiType != models.TaskAPITypeResponses {
					c.Writer.Write([]byte("data: [DONE]\n\n"))
					flusher.Flush()
				}
				return
			}
			frames, stop, err := encoder.Encode(provider.StreamEvent{
				Kind:    provider.StreamEventKind(event.Kind),
				Payload: event.Payload,
			})
			if err != nil {
				log.Errorf("provider stream encode failed for job %d: %v", job.ID, err)
				return
			}
			for _, frame := range frames {
				if _, err := c.Writer.Write(frame); err != nil {
					return
				}
				flusher.Flush()
			}
			if stop || event.Error {
				return
			}
		}
	}
}

func writePaymentRequired(c *gin.Context) {
	c.JSON(http.StatusPaymentRequired, gin.H{
		"error": gin.H{
			"message": "Insufficient Credits",
			"type":    "insufficient_quota",
			"code":    "insufficient_credits",
		},
	})
}

func writeTimeoutError(c *gin.Context) {
	c.JSON(http.StatusGatewayTimeout, gin.H{
		"error": gin.H{
			"message": "Request timed out waiting for a Shared API Key",
			"type":    "timeout",
			"code":    "timeout",
		},
	})
}
