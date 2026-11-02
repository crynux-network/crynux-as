package responses

import (
	"context"
	"crynux_as/api/llm/vramlimit"
	"crynux_as/config"
	"crynux_as/llmadapter"
	"crynux_as/models"
	"crynux_as/relay"
	"crynux_as/service"
	"crynux_as/service/huggingface"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

const projectContextKey = "llm_project"

func CreateResponse(c *gin.Context) {
	project := getProject(c)
	if project == nil {
		writeAuthError(c, "unauthorized")
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		writeRequestBodyReadError(c, err)
		return
	}

	appCfg := config.GetConfig()
	req, err := llmadapter.ParseResponsesRequest(body)
	if err != nil {
		writeLLMAdapterError(c, err)
		return
	}

	db := config.GetDB()
	var history []models.Message
	if strings.TrimSpace(req.PreviousResponseID) != "" {
		prevJob, err := service.LoadPreviousResponsesJob(
			c.Request.Context(),
			db,
			project.ID,
			req.PreviousResponseID,
			appCfg.LLM.JobRetentionDays,
		)
		if err != nil {
			if errors.Is(err, service.ErrPreviousResponsesInvalid) {
				writeLLMAdapterError(c, llmadapter.NewValidationError("previous_response_id", "previous response is invalid"))
				return
			}
			writeLLMAdapterError(c, err)
			return
		}
		if prevJob.RawResultJSON == nil {
			writeClientError(c, http.StatusBadRequest, "previous_response_id: previous response is invalid")
			return
		}
		history, err = llmadapter.BuildResponsesHistoryFromPreviousJob(prevJob.PublicID, prevJob.TaskArgsJSON, *prevJob.RawResultJSON)
		if err != nil {
			writeLLMAdapterError(c, err)
			return
		}
	}

	taskArgsJSON, err := llmadapter.BuildResponsesTaskArgsWithHistory(req, history, int(appCfg.LLM.DefaultMaxTokens))
	if err != nil {
		writeLLMAdapterError(c, err)
		return
	}
	execution := service.ResolveLLMModelExecution(req.Model, req.Dtype, req.QuantizationConfig)
	taskArgsJSON, err = service.ApplyLLMExecutionToTaskArgsJSON(taskArgsJSON, execution)
	if err != nil {
		writeClientError(c, http.StatusBadRequest, "failed to apply model execution parameters")
		return
	}
	if err := models.ValidateCrynuxTaskArgsSize(taskArgsJSON, appCfg.LLM.MaxCrynuxTaskArgsBytes); err != nil {
		writeClientError(c, http.StatusBadRequest, err.Error())
		return
	}

	userVram, err := vramlimit.ResolveUserVramLimit(req.VramLimit, c.Param("vram_limit"))
	if err != nil {
		writeClientError(c, http.StatusBadRequest, err.Error())
		return
	}
	effectiveVram := vramlimit.ResolveEffectiveVram(req.Model, userVram)

	if err := service.ValidateTaskModel(
		c.Request.Context(),
		huggingface.ModelKindLLM,
		req.Model,
	); err != nil {
		if errors.Is(err, service.ErrBaseModelCheckUnavailable) {
			writeServerError(c)
			return
		}
		if errors.Is(err, service.ErrInvalidBaseModel) {
			writeClientError(c, http.StatusBadRequest, err.Error())
			return
		}
		writeServerError(c)
		return
	}

	estPrompt := service.EstimatePromptTokensFromResponsesBody(body)
	maxCompletion := service.ResolveMaxCompletionTokens(nil, req.MaxOutputTokens, appCfg.LLM.DefaultMaxTokens)
	priorityGwei, err := service.ResolveEffectivePriorityGwei(project)
	if err != nil {
		log.Errorf("Resolve effective priority failed for project %d: %v", project.ID, err)
		writeServerError(c)
		return
	}
	query := relay.ExecutionTimeQuery{
		Model:              req.Model,
		Dtype:              string(execution.Dtype),
		QuantizationConfig: execution.QuantizationConfig,
		MinVRAM:            &effectiveVram,
	}
	taskFee, err := service.EstimateLLMTaskFeeWithQuery(
		c.Request.Context(),
		project,
		query,
		effectiveVram,
		priorityGwei,
		estPrompt,
		maxCompletion,
	)
	if err != nil {
		if errors.Is(err, service.ErrTaskFeeBelowMinimum) ||
			errors.Is(err, service.ErrAutoMaxCreditsInsufficient) ||
			errors.Is(err, service.ErrMaxCreditsExceeded) {
			writeClientError(c, http.StatusBadRequest, err.Error())
			return
		}
		log.Errorf("Task fee estimation failed for project %d model %s: %v", project.ID, req.Model, err)
		writeServerError(c)
		return
	}
	priorityGwei = taskFee.PriorityGwei

	if err := service.ValidateOptionalQueueTimeout(req.QueueTimeout); err != nil {
		writeClientError(c, http.StatusBadRequest, err.Error())
		return
	}

	jobInput := service.CreateTaskJobInput{
		Project:               project,
		APIType:               models.TaskAPITypeResponses,
		Model:                 req.Model,
		BilledVram:            effectiveVram,
		Background:            req.Background,
		RequestBody:           body,
		TaskArgsJSON:          taskArgsJSON,
		PriorityGwei:          priorityGwei,
		TaskFeeGwei:           taskFee.TaskFeeGwei,
		MedianPriorityGwei:    taskFee.MedianPriorityGwei,
		EstimatedNodeSeconds:  float64Ptr(taskFee.EstimatedNodeSeconds),
		VramWeight:            float64Ptr(taskFee.VramWeight),
		ConstantSeconds:       float64Ptr(taskFee.ConstantSeconds),
		SecondsPerInputToken:  float64Ptr(taskFee.SecondsPerInputToken),
		SecondsPerOutputToken: float64Ptr(taskFee.SecondsPerOutputToken),
		CreditsToLock:         taskFee.Credits,
		QueueTimeout:          req.QueueTimeout,
	}

	job, err := service.ReserveCreditsAndCreateTaskJob(c.Request.Context(), db, jobInput)
	if err != nil {
		if errors.Is(err, service.ErrInsufficientBalance) {
			writeClientError(c, http.StatusPaymentRequired, "insufficient credits balance")
			return
		}
		log.Errorf("Create LLM job failed for project %d: %v", project.ID, err)
		writeServerError(c)
		return
	}

	if req.Background {
		writeResponsesJob(c, job, llmadapter.ResponsesStatusQueued)
		return
	}

	finalJob, err := service.WaitForTaskJob(c.Request.Context(), db, job.ID, service.ResolveTaskJobWaitTimeout(c.Request.Header))
	if err != nil {
		if errors.Is(err, service.ErrTaskJobWaitTimeout) {
			writeClientError(c, http.StatusGatewayTimeout, "request timed out waiting for task completion")
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Errorf("Wait for LLM job %d failed: %v", job.ID, err)
		writeServerError(c)
		return
	}

	if finalJob.Status == models.TaskJobStatusFailed {
		payload, err := loadResponsesPayload(finalJob)
		if err != nil {
			log.Errorf("format failed response job %d: %v", finalJob.ID, err)
			writeServerError(c)
			return
		}
		c.Data(http.StatusOK, "application/json", payload)
		return
	}

	if finalJob.FormattedResultJSON == nil {
		writeServerError(c)
		return
	}
	c.Data(http.StatusOK, "application/json", []byte(*finalJob.FormattedResultJSON))
}

func GetResponse(c *gin.Context) {
	project := getProject(c)
	if project == nil {
		writeAuthError(c, "unauthorized")
		return
	}

	publicID := strings.TrimSpace(c.Param("response_id"))
	if publicID == "" {
		writeClientError(c, http.StatusBadRequest, "response id is required")
		return
	}

	db := config.GetDB()
	job, err := service.GetTaskJobByPublicID(c.Request.Context(), db, project.ID, publicID)
	if err != nil {
		if errors.Is(err, service.ErrTaskJobNotFound) {
			writeClientError(c, http.StatusNotFound, "response not found")
			return
		}
		log.Errorf("load response job for project %d public_id %s: %v", project.ID, publicID, err)
		writeServerError(c)
		return
	}
	if job.APIType != models.TaskAPITypeResponses {
		writeClientError(c, http.StatusNotFound, "response not found")
		return
	}
	if service.IsResponsesJobExpired(job, config.GetConfig().LLM.JobRetentionDays) {
		writeClientError(c, http.StatusNotFound, "response not found")
		return
	}

	payload, err := loadResponsesPayload(job)
	if err != nil {
		log.Errorf("format response job %d failed: %v", job.ID, err)
		writeServerError(c)
		return
	}

	c.Data(http.StatusOK, "application/json", payload)
}

func getProject(c *gin.Context) *models.Project {
	v, ok := c.Get(projectContextKey)
	if !ok {
		return nil
	}
	project, _ := v.(*models.Project)
	return project
}

func writeAuthError(c *gin.Context, message string) {
	c.JSON(http.StatusUnauthorized, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}

func writeClientError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}

func writeRequestBodyReadError(c *gin.Context, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeClientError(c, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	writeClientError(c, http.StatusBadRequest, "failed to read request body")
}

func writeServerError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": gin.H{
			"message": "internal server error",
			"type":    "server_error",
		},
	})
}

func writeLLMAdapterError(c *gin.Context, err error) {
	var validationErr *llmadapter.ValidationError
	if errors.As(err, &validationErr) {
		message := validationErr.Message
		if validationErr.Field != "" {
			message = validationErr.Field + ": " + validationErr.Message
		}
		writeClientError(c, http.StatusBadRequest, message)
		return
	}
	writeClientError(c, http.StatusBadRequest, err.Error())
}

func writeResponsesJob(c *gin.Context, job *models.TaskJob, status string) {
	payload, err := llmadapter.FormatResponsesPendingObject(llmadapter.ResponsesObjectParams{
		ID:         job.PublicID,
		Model:      job.Model,
		CreatedAt:  job.CreatedAt.Unix(),
		Status:     status,
		Background: job.Background,
	})
	if err != nil {
		log.Errorf("format pending response for job %d failed: %v", job.ID, err)
		writeServerError(c)
		return
	}
	c.Data(http.StatusOK, "application/json", payload)
}

func loadResponsesPayload(job *models.TaskJob) ([]byte, error) {
	if job.Status == models.TaskJobStatusCompleted && job.FormattedResultJSON != nil {
		return []byte(*job.FormattedResultJSON), nil
	}
	params := responsesObjectParamsFromJob(job)
	if job.Status == models.TaskJobStatusFailed {
		return llmadapter.FormatResponsesObject(params, nil)
	}
	return llmadapter.FormatResponsesPendingObject(params)
}

func responsesObjectParamsFromJob(job *models.TaskJob) llmadapter.ResponsesObjectParams {
	status := llmadapter.ResponsesStatusQueued
	switch job.Status {
	case models.TaskJobStatusSubmitted, models.TaskJobStatusInProgress:
		status = llmadapter.ResponsesStatusInProgress
	case models.TaskJobStatusCompleted:
		status = llmadapter.ResponsesStatusCompleted
	case models.TaskJobStatusFailed:
		status = llmadapter.ResponsesStatusFailed
	}

	params := llmadapter.ResponsesObjectParams{
		ID:         job.PublicID,
		Model:      job.Model,
		CreatedAt:  job.CreatedAt.Unix(),
		Status:     status,
		Background: job.Background,
	}
	if job.Status == models.TaskJobStatusFailed && job.ErrorMessage != nil {
		params.Error = &llmadapter.ResponsesAPIError{
			Message: *job.ErrorMessage,
			Type:    "server_error",
		}
	}
	return params
}

func float64Ptr(v float64) *float64 {
	copied := v
	return &copied
}

func extractModelFromBody(body []byte) string {
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return llmadapter.NormalizeModelID(payload.Model)
}

