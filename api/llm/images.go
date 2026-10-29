package llm

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
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

const imagesWeiPerGwei = int64(1_000_000_000)

func ImagesGenerations(c *gin.Context) {
	project := GetProject(c)
	if project == nil {
		writeAuthError(c, "unauthorized")
		return
	}
	acceptedAt := time.Now()
	apiType := models.TaskAPITypeImagesGenerations

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, "", 0, acceptedAt, time.Now(), nil, nil)
		writeClientError(c, http.StatusBadRequest, "failed to read request body")
		return
	}

	req, err := llmadapter.ParseImagesGenerationsRequest(body)
	if err != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, extractModelFromBody(body), 0, acceptedAt, time.Now(), nil, nil)
		writeLLMAdapterError(c, err)
		return
	}

	taskArgsJSON, err := llmadapter.BuildImagesGenerationsTaskArgs(req)
	if err != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, 0, acceptedAt, time.Now(), nil, nil)
		writeServerError(c)
		return
	}
	taskArgsJSON, billedVram, err := service.ApplyImageExecutionToTaskArgsJSON(taskArgsJSON, req.Model)
	if err != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, 0, acceptedAt, time.Now(), nil, nil)
		if errors.Is(err, service.ErrImageExecutionUnresolved) {
			writeClientError(c, http.StatusBadRequest, err.Error())
			return
		}
		writeClientError(c, http.StatusBadRequest, "failed to apply model execution parameters")
		return
	}
	userVram, err := vramlimit.ResolveUserVramLimit(req.VramLimit, c.Param("vram_limit"))
	if err != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, 0, acceptedAt, time.Now(), nil, nil)
		writeClientError(c, http.StatusBadRequest, err.Error())
		return
	}
	if userVram != nil {
		billedVram = *userVram
	}
	if validationErr, err := models.ValidateTaskArgsJsonStr(taskArgsJSON, models.TaskTypeImage); err != nil {
		log.Errorf("Validate image task args for project %d failed: %v", project.ID, err)
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, billedVram, acceptedAt, time.Now(), nil, nil)
		writeServerError(c)
		return
	} else if validationErr != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, billedVram, acceptedAt, time.Now(), nil, nil)
		writeClientError(c, http.StatusBadRequest, "invalid task_args: "+validationErr.Error())
		return
	}

	if err := service.ValidateTaskModel(c.Request.Context(), huggingface.ModelKindSD, req.Model); err != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, billedVram, acceptedAt, time.Now(), nil, nil)
		writeTaskModelValidationError(c, err)
		return
	}

	dtype, quantConfig := imageExecutionFromTaskArgs(taskArgsJSON)
	priorityGwei, err := service.ResolveEffectivePriorityGwei(project)
	if err != nil {
		log.Errorf("Resolve effective priority failed for project %d: %v", project.ID, err)
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, billedVram, acceptedAt, time.Now(), nil, nil)
		writeServerError(c)
		return
	}
	minVram := billedVram
	estimate, err := service.EstimateImageTaskFee(c.Request.Context(), service.CalcImageTaskFeeInput{
		Project:  project,
		TaskArgs: taskArgsJSON,
		Query: relay.ExecutionTimeQuery{
			Model:              req.Model,
			Dtype:              dtype,
			QuantizationConfig: quantConfig,
			MinVRAM:            &minVram,
		},
		PriorityGwei:  priorityGwei,
		EffectiveVram: billedVram,
	})
	if err != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, billedVram, acceptedAt, time.Now(), priorityGwei, nil)
		if errors.Is(err, service.ErrTaskFeeBelowMinimum) {
			writeClientError(c, http.StatusBadRequest, err.Error())
			return
		}
		log.Errorf("Image task fee estimation failed for project %d model %s: %v", project.ID, req.Model, err)
		writeServerError(c)
		return
	}
	priorityGwei = estimate.PriorityGwei

	taskFeeWei := new(big.Int).Mul(estimate.TaskFeeGwei, big.NewInt(imagesWeiPerGwei))
	db := config.GetDB()
	job, err := service.ReserveCreditsAndCreateRawTaskJob(c.Request.Context(), db, service.CreateRawTaskJobInput{
		Project:       project,
		TaskType:      models.TaskTypeImage,
		APIType:       apiType,
		Model:         req.Model,
		TaskArgsJSON:  taskArgsJSON,
		MinVram:       &minVram,
		PriorityGwei:  priorityGwei,
		BilledVram:    billedVram,
		TaskFeeWei:    taskFeeWei,
		ImageCredits:  estimate.Credits,
		CreditsToLock: estimate.Credits,
		QueueTimeout:  req.QueueTimeout,
	})
	if err != nil {
		if errors.Is(err, service.ErrInsufficientBalance) {
			_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, billedVram, acceptedAt, time.Now(), priorityGwei, estimate)
			writeClientError(c, http.StatusPaymentRequired, "insufficient credits balance")
			return
		}
		log.Errorf("Create images generations job failed for project %d: %v", project.ID, err)
		writeServerError(c)
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
		log.Errorf("Wait for images generations job %d failed: %v", job.ID, err)
		writeServerError(c)
		return
	}

	if finalJob.Status == models.TaskJobStatusFailed {
		msg := "task failed"
		if finalJob.ErrorMessage != nil {
			msg = *finalJob.ErrorMessage
		}
		writeClientError(c, http.StatusInternalServerError, msg)
		return
	}

	pngs, err := service.ReadImageResultPNGs(finalJob.ID, uint64(req.N))
	if err != nil {
		log.Errorf("Read image results for job %d failed: %v", finalJob.ID, err)
		writeServerError(c)
		return
	}
	created := time.Now().Unix()
	if finalJob.CompletedAt != nil {
		created = finalJob.CompletedAt.Unix()
	}
	responseBytes, err := llmadapter.FormatImagesGenerationsResponse(created, req, pngs)
	if err != nil {
		log.Errorf("Format images generations response for job %d failed: %v", finalJob.ID, err)
		writeServerError(c)
		return
	}
	c.Data(http.StatusOK, "application/json", responseBytes)
}

func ImagesEdits(c *gin.Context) {
	project := GetProject(c)
	if project == nil {
		writeAuthError(c, "unauthorized")
		return
	}
	acceptedAt := time.Now()
	apiType := models.TaskAPITypeImagesEdits

	req, modelHint, err := parseImagesEditsRequest(c)
	if err != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, modelHint, 0, acceptedAt, time.Now(), nil, nil)
		writeLLMAdapterError(c, err)
		return
	}

	taskArgsJSON, err := llmadapter.BuildImagesEditsTaskArgs(req)
	if err != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, 0, acceptedAt, time.Now(), nil, nil)
		writeServerError(c)
		return
	}
	taskArgsJSON, billedVram, err := service.ApplyImageExecutionToTaskArgsJSON(taskArgsJSON, req.Model)
	if err != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, 0, acceptedAt, time.Now(), nil, nil)
		if errors.Is(err, service.ErrImageExecutionUnresolved) {
			writeClientError(c, http.StatusBadRequest, err.Error())
			return
		}
		writeClientError(c, http.StatusBadRequest, "failed to apply model execution parameters")
		return
	}
	userVram, err := vramlimit.ResolveUserVramLimit(req.VramLimit, c.Param("vram_limit"))
	if err != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, 0, acceptedAt, time.Now(), nil, nil)
		writeClientError(c, http.StatusBadRequest, err.Error())
		return
	}
	if userVram != nil {
		billedVram = *userVram
	}
	if err := models.ValidateImageTaskArgsImagesSize(taskArgsJSON); err != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, billedVram, acceptedAt, time.Now(), nil, nil)
		writeClientError(c, http.StatusBadRequest, err.Error())
		return
	}
	if validationErr, err := models.ValidateTaskArgsJsonStr(taskArgsJSON, models.TaskTypeImage); err != nil {
		log.Errorf("Validate image edit task args for project %d failed: %v", project.ID, err)
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, billedVram, acceptedAt, time.Now(), nil, nil)
		writeServerError(c)
		return
	} else if validationErr != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, billedVram, acceptedAt, time.Now(), nil, nil)
		writeClientError(c, http.StatusBadRequest, "invalid task_args: "+validationErr.Error())
		return
	}

	if err := service.ValidateTaskModel(c.Request.Context(), huggingface.ModelKindSD, req.Model); err != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, billedVram, acceptedAt, time.Now(), nil, nil)
		writeTaskModelValidationError(c, err)
		return
	}

	dtype, quantConfig := imageExecutionFromTaskArgs(taskArgsJSON)
	priorityGwei, err := service.ResolveEffectivePriorityGwei(project)
	if err != nil {
		log.Errorf("Resolve effective priority failed for project %d: %v", project.ID, err)
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, billedVram, acceptedAt, time.Now(), nil, nil)
		writeServerError(c)
		return
	}
	minVram := billedVram
	estimate, err := service.EstimateImageTaskFee(c.Request.Context(), service.CalcImageTaskFeeInput{
		Project:  project,
		TaskArgs: taskArgsJSON,
		Query: relay.ExecutionTimeQuery{
			Model:              req.Model,
			Dtype:              dtype,
			QuantizationConfig: quantConfig,
			MinVRAM:            &minVram,
		},
		PriorityGwei:  priorityGwei,
		EffectiveVram: billedVram,
	})
	if err != nil {
		_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, billedVram, acceptedAt, time.Now(), priorityGwei, nil)
		if errors.Is(err, service.ErrTaskFeeBelowMinimum) {
			writeClientError(c, http.StatusBadRequest, err.Error())
			return
		}
		log.Errorf("Image edit task fee estimation failed for project %d model %s: %v", project.ID, req.Model, err)
		writeServerError(c)
		return
	}
	priorityGwei = estimate.PriorityGwei

	taskFeeWei := new(big.Int).Mul(estimate.TaskFeeGwei, big.NewInt(imagesWeiPerGwei))
	db := config.GetDB()
	job, err := service.ReserveCreditsAndCreateRawTaskJob(c.Request.Context(), db, service.CreateRawTaskJobInput{
		Project:       project,
		TaskType:      models.TaskTypeImage,
		APIType:       apiType,
		Model:         req.Model,
		TaskArgsJSON:  taskArgsJSON,
		MinVram:       &minVram,
		PriorityGwei:  priorityGwei,
		BilledVram:    billedVram,
		TaskFeeWei:    taskFeeWei,
		ImageCredits:  estimate.Credits,
		CreditsToLock: estimate.Credits,
		QueueTimeout:  req.QueueTimeout,
	})
	if err != nil {
		if errors.Is(err, service.ErrInsufficientBalance) {
			_ = recordFailedImageCall(c.Request.Context(), project, apiType, req.Model, billedVram, acceptedAt, time.Now(), priorityGwei, estimate)
			writeClientError(c, http.StatusPaymentRequired, "insufficient credits balance")
			return
		}
		log.Errorf("Create images edits job failed for project %d: %v", project.ID, err)
		writeServerError(c)
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
		log.Errorf("Wait for images edits job %d failed: %v", job.ID, err)
		writeServerError(c)
		return
	}

	if finalJob.Status == models.TaskJobStatusFailed {
		msg := "task failed"
		if finalJob.ErrorMessage != nil {
			msg = *finalJob.ErrorMessage
		}
		writeClientError(c, http.StatusInternalServerError, msg)
		return
	}

	pngs, err := service.ReadImageResultPNGs(finalJob.ID, uint64(req.N))
	if err != nil {
		log.Errorf("Read image results for edit job %d failed: %v", finalJob.ID, err)
		writeServerError(c)
		return
	}
	created := time.Now().Unix()
	if finalJob.CompletedAt != nil {
		created = finalJob.CompletedAt.Unix()
	}
	responseBytes, err := llmadapter.FormatImagesEditsResponse(created, req, pngs)
	if err != nil {
		log.Errorf("Format images edits response for job %d failed: %v", finalJob.ID, err)
		writeServerError(c)
		return
	}
	c.Data(http.StatusOK, "application/json", responseBytes)
}

func parseImagesEditsRequest(c *gin.Context) (llmadapter.ImagesEditsRequest, string, error) {
	contentType := c.ContentType()
	switch {
	case strings.HasPrefix(contentType, "multipart/form-data"):
		if err := c.Request.ParseMultipartForm(8 << 20); err != nil {
			return llmadapter.ImagesEditsRequest{}, "", llmadapter.NewValidationError("", "invalid multipart form")
		}
		req, err := llmadapter.ParseImagesEditsMultipartRequest(c.Request.MultipartForm)
		if err != nil {
			model := ""
			if c.Request.MultipartForm != nil {
				if values := c.Request.MultipartForm.Value["model"]; len(values) > 0 {
					model = llmadapter.NormalizeModelID(values[len(values)-1])
				}
			}
			return llmadapter.ImagesEditsRequest{}, model, err
		}
		return req, req.Model, nil
	case contentType == "" || strings.HasPrefix(contentType, "application/json"):
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			return llmadapter.ImagesEditsRequest{}, "", llmadapter.NewValidationError("", "failed to read request body")
		}
		req, err := llmadapter.ParseImagesEditsJSONRequest(body)
		if err != nil {
			return llmadapter.ImagesEditsRequest{}, extractModelFromBody(body), err
		}
		return req, req.Model, nil
	default:
		return llmadapter.ImagesEditsRequest{}, "", llmadapter.NewValidationError("", "content type must be application/json or multipart/form-data")
	}
}

func imageExecutionFromTaskArgs(taskArgsJSON string) (dtype string, quantConfig any) {
	var payload struct {
		Dtype              string `json:"dtype"`
		QuantizationConfig any    `json:"quantization_config"`
	}
	if err := json.Unmarshal([]byte(taskArgsJSON), &payload); err != nil {
		return "", nil
	}
	return strings.ToLower(strings.TrimSpace(payload.Dtype)), payload.QuantizationConfig
}

func recordFailedImageCall(
	ctx context.Context,
	project *models.Project,
	apiType models.TaskAPIType,
	model string,
	billedVram uint64,
	acceptedAt, completedAt time.Time,
	priorityGwei *big.Int,
	taskFee *service.CalcTaskFeeResult,
) error {
	if acceptedAt.IsZero() {
		acceptedAt = completedAt
	}
	if completedAt.IsZero() {
		completedAt = time.Now()
		if acceptedAt.IsZero() {
			acceptedAt = completedAt
		}
	}
	if priorityGwei == nil {
		priorityGwei = &project.PriorityGwei.Int
	}
	in := service.RecordTaskCallInput{
		UserID:               project.UserID,
		ProjectID:            project.ID,
		TaskType:             models.TaskTypeImage,
		APIType:              apiType,
		Model:                model,
		PriorityGwei:         priorityGwei,
		TokenUsageApplicable: false,
		Status:               models.TaskCallStatusFailed,
		Credits:              big.NewInt(0),
		AcceptedAt:           acceptedAt,
		CompletedAt:          completedAt,
		BilledVram:           billedVram,
		Charge:               false,
	}
	if taskFee != nil {
		in.TaskFeeGwei = taskFee.TaskFeeGwei
		in.MedianPriorityGwei = taskFee.MedianPriorityGwei
		estimated := taskFee.EstimatedNodeSeconds
		weight := taskFee.VramWeight
		in.EstimatedNodeSeconds = &estimated
		in.VramWeight = &weight
	}
	_, err := service.ProcessTaskCall(ctx, config.GetDB(), in)
	return err
}
