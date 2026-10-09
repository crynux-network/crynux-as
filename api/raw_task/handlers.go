package raw_task

import (
	"context"
	"crynux_as/api/llm"
	"crynux_as/bridge"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/relay"
	"crynux_as/service"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const weiPerGwei = int64(1_000_000_000)

type llmArgsForPricing struct {
	Model            string                      `json:"model"`
	Dtype            string                      `json:"dtype"`
	QuantizeBits     *uint64                     `json:"quantize_bits"`
	GenerationConfig *models.GPTGenerationConfig `json:"generation_config"`
}

type imageModelArg struct {
	Name    string `json:"name"`
	Variant string `json:"variant"`
}

type imageArgsForPricing struct {
	BaseModel    json.RawMessage `json:"base_model"`
	Dtype        string          `json:"dtype"`
	QuantizeBits *uint64         `json:"quantize_bits"`
}

func CreateTask(c *gin.Context) {
	project := llm.GetProject(c)
	var request CreateTaskRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid request body")
		return
	}
	prepared, err := prepareTask(c.Request.Context(), project, request)
	if err != nil {
		recordFailure(c.Request.Context(), project, "", 0)
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	account, err := loadAccount(c.Request.Context(), project.UserID)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	credits, _ := new(big.Int).SetString(prepared.credits, 10)
	if err := service.EnsureSufficientBalance(&account.Balance.Int, credits); err != nil {
		recordFailure(c.Request.Context(), project, prepared.model, prepared.billedVram)
		writeError(c, http.StatusPaymentRequired, "insufficient credits balance")
		return
	}
	job, err := createPreparedTask(c.Request.Context(), project, prepared)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": taskView(job)})
}

func BatchCreate(c *gin.Context) {
	project := llm.GetProject(c)
	var request BatchCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(request.Tasks) == 0 || len(request.Tasks) > maxBatchSize {
		writeError(c, http.StatusBadRequest, "tasks must contain between 1 and 100 items")
		return
	}
	account, err := loadAccount(c.Request.Context(), project.UserID)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	remaining := new(big.Int).Set(&account.Balance.Int)
	results := make([]BatchCreateItem, 0, len(request.Tasks))
	for index, item := range request.Tasks {
		prepared, err := prepareTask(c.Request.Context(), project, item)
		if err != nil {
			recordFailure(c.Request.Context(), project, "", 0)
			results = append(results, BatchCreateItem{Index: index, Error: err.Error()})
			continue
		}
		credits, _ := new(big.Int).SetString(prepared.credits, 10)
		if remaining.Cmp(credits) < 0 {
			recordFailure(c.Request.Context(), project, prepared.model, prepared.billedVram)
			results = append(results, BatchCreateItem{Index: index, Error: "insufficient credits balance"})
			continue
		}
		job, err := createPreparedTask(c.Request.Context(), project, prepared)
		if err != nil {
			results = append(results, BatchCreateItem{Index: index, Error: "failed to create task"})
			log.Errorf("Create raw task failed for project %d: %v", project.ID, err)
			continue
		}
		remaining.Sub(remaining, credits)
		view := taskView(job)
		results = append(results, BatchCreateItem{Index: index, ClientTask: &view})
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": results})
}

func GetTask(c *gin.Context) {
	project := llm.GetProject(c)
	id, err := strconv.ParseUint(c.Param("client_task_id"), 10, 64)
	if err != nil || id == 0 {
		writeError(c, http.StatusBadRequest, "invalid client_task_id")
		return
	}
	job, err := loadProjectTask(c.Request.Context(), project.ID, uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "task not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": taskView(job)})
}

func BatchStatus(c *gin.Context) {
	project := llm.GetProject(c)
	var request BatchStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(request.ClientTaskIDs) == 0 || len(request.ClientTaskIDs) > maxBatchSize {
		writeError(c, http.StatusBadRequest, "client_task_ids must contain between 1 and 100 items")
		return
	}
	unique := make([]uint, 0, len(request.ClientTaskIDs))
	seen := map[uint]struct{}{}
	for _, id := range request.ClientTaskIDs {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			unique = append(unique, id)
		}
	}
	var jobs []models.TaskJob
	if err := config.GetDB().WithContext(c.Request.Context()).
		Where("project_id = ? AND id IN ?", project.ID, unique).
		Find(&jobs).Error; err != nil {
		writeInternalError(c, err)
		return
	}
	byID := make(map[uint]*models.TaskJob, len(jobs))
	for i := range jobs {
		byID[jobs[i].ID] = &jobs[i]
	}
	results := make([]BatchStatusItem, 0, len(request.ClientTaskIDs))
	for _, id := range request.ClientTaskIDs {
		job := byID[id]
		if job == nil {
			results = append(results, BatchStatusItem{ClientTaskID: id, Error: "task not found"})
			continue
		}
		view := taskView(job)
		results = append(results, BatchStatusItem{ClientTaskID: id, ClientTask: &view})
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": results})
}

func DownloadLLM(c *gin.Context) {
	downloadResult(c, models.TaskTypeLLM, 0)
}

func DownloadImage(c *gin.Context) {
	index, err := strconv.ParseUint(c.Param("index"), 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid image index")
		return
	}
	downloadResult(c, models.TaskTypeImage, index)
}

func downloadResult(c *gin.Context, expectedType models.TaskType, index uint64) {
	project := llm.GetProject(c)
	id, err := strconv.ParseUint(c.Param("client_task_id"), 10, 64)
	if err != nil || id == 0 {
		writeError(c, http.StatusBadRequest, "invalid client_task_id")
		return
	}
	job, err := loadProjectTask(c.Request.Context(), project.ID, uint(id))
	if err != nil {
		writeError(c, http.StatusNotFound, "task not found")
		return
	}
	if job.TaskType != expectedType {
		writeError(c, http.StatusBadRequest, "task result type does not match the endpoint")
		return
	}
	if job.Status != models.TaskJobStatusCompleted || job.BridgeClientTaskID == nil {
		writeError(c, http.StatusConflict, "task is not successful")
		return
	}
	cfg := config.GetConfig()
	client := bridge.NewRawTaskClient(cfg.Bridge.BaseURL, cfg.Bridge.APIKey)
	if expectedType == models.TaskTypeLLM {
		body, err := client.DownloadLLMResult(c.Request.Context(), *job.BridgeClientTaskID)
		if err != nil {
			writeInternalError(c, err)
			return
		}
		c.Header("Content-Disposition", "attachment; filename=0.json")
		c.Data(http.StatusOK, "application/json", body)
		return
	}
	result, err := client.DownloadImageResult(c.Request.Context(), *job.BridgeClientTaskID, index)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	disposition := result.ContentDisposition
	if disposition == "" {
		disposition = fmt.Sprintf("attachment; filename=%d.png", index)
	}
	c.Header("Content-Disposition", disposition)
	c.Data(http.StatusOK, "image/png", result.Body)
}

func prepareTask(ctx context.Context, project *models.Project, request CreateTaskRequest) (*preparedTask, error) {
	if project == nil || request.TaskType == nil {
		return nil, errors.New("task_type is required")
	}
	if *request.TaskType != models.TaskTypeImage && *request.TaskType != models.TaskTypeLLM {
		return nil, errors.New("task_type must be 0 or 1")
	}
	if len(request.TaskFee) > 0 {
		return nil, errors.New("task_fee is managed by Crynux AS and must not be provided")
	}
	if !json.Valid([]byte(request.TaskArgs)) {
		return nil, errors.New("task_args must be valid JSON")
	}
	hasGPUName := strings.TrimSpace(request.RequiredGPU) != ""
	hasGPUVram := request.RequiredGPUVram > 0
	if request.MinVram != nil && (hasGPUName || hasGPUVram) {
		return nil, errors.New("min_vram must not be combined with required_gpu or required_gpu_vram")
	}
	if hasGPUName != hasGPUVram {
		return nil, errors.New("required_gpu and required_gpu_vram must be provided together")
	}

	model, variant, dtype, quantizeBits, maxTokens, err := parseTaskArgs(*request.TaskType, request.TaskArgs)
	if err != nil {
		return nil, err
	}
	minVram := request.MinVram
	if minVram != nil && *minVram == 0 {
		return nil, errors.New("min_vram must be a positive integer")
	}
	if minVram == nil && !hasGPUName {
		resolved := config.GetConfig().LLM.DefaultVramLimit
		if *request.TaskType == models.TaskTypeImage {
			if loaded, ok := service.GetLoadedSDModel(model, variant); ok && loaded.MinVRAM > 0 {
				resolved = loaded.MinVRAM
			}
		} else if loaded, ok := service.GetLoadedLLMModel(model); ok && loaded.MinVRAM > 0 {
			resolved = loaded.MinVRAM
		}
		minVram = &resolved
	}
	billedVram := request.RequiredGPUVram
	if billedVram == 0 {
		billedVram = *minVram
	}
	priority, err := service.ResolveEffectivePriorityGwei(project)
	if err != nil {
		return nil, err
	}
	query := relay.ExecutionTimeQuery{
		Model: model, Variant: variant, Dtype: dtype, QuantizeBits: quantizeBits,
		MinVRAM: minVram, GPUName: request.RequiredGPU,
	}
	if hasGPUVram {
		query.GPUVRAM = &request.RequiredGPUVram
	}
	var estimate *service.CalcTaskFeeResult
	var llmBilling *models.LLMTaskBillingData
	if *request.TaskType == models.TaskTypeImage {
		estimate, err = service.EstimateImageTaskFee(ctx, service.CalcImageTaskFeeInput{
			TaskArgs: request.TaskArgs, Query: query, PriorityGwei: priority,
			EffectiveVram: billedVram,
		})
	} else {
		promptTokens := uint64((utf8.RuneCountInString(request.TaskArgs) + 3) / 4)
		estimate, err = service.EstimateLLMTaskFeeWithQuery(ctx, query, billedVram, priority, promptTokens, maxTokens)
		if err == nil {
			llmBilling = &models.LLMTaskBillingData{
				VramWeight: estimate.VramWeight, ConstantSeconds: estimate.ConstantSeconds,
				SecondsPerInputToken:  estimate.SecondsPerInputToken,
				SecondsPerOutputToken: estimate.SecondsPerOutputToken,
				CreditsPerGwei:        config.GetConfig().LLM.CreditsPerGwei,
			}
		}
	}
	if err != nil {
		return nil, err
	}
	taskFeeWei := new(big.Int).Mul(estimate.TaskFeeGwei, big.NewInt(weiPerGwei))
	return &preparedTask{
		request: request, model: model, taskArgs: request.TaskArgs, minVram: minVram,
		billedVram: billedVram, priority: priority.String(),
		taskFeeWei: taskFeeWei.String(), credits: estimate.Credits.String(), llmBilling: llmBilling,
	}, nil
}

func parseTaskArgs(taskType models.TaskType, raw string) (model, variant, dtype string, quantizeBits *uint64, maxTokens uint64, err error) {
	if taskType == models.TaskTypeLLM {
		var args llmArgsForPricing
		if err = json.Unmarshal([]byte(raw), &args); err != nil {
			return
		}
		model = strings.TrimSpace(args.Model)
		dtype = args.Dtype
		quantizeBits = args.QuantizeBits
		if args.GenerationConfig != nil && args.GenerationConfig.MaxNewTokens > 0 {
			maxTokens = uint64(args.GenerationConfig.MaxNewTokens)
		} else {
			maxTokens = config.GetConfig().LLM.DefaultMaxTokens
		}
	} else {
		var args imageArgsForPricing
		if err = json.Unmarshal([]byte(raw), &args); err != nil {
			return
		}
		dtype = args.Dtype
		quantizeBits = args.QuantizeBits
		var name string
		if json.Unmarshal(args.BaseModel, &name) == nil {
			model = name
		} else {
			var base imageModelArg
			if err = json.Unmarshal(args.BaseModel, &base); err != nil {
				return
			}
			model, variant = base.Name, base.Variant
		}
	}
	if strings.TrimSpace(model) == "" {
		err = errors.New("task_args model is required")
	}
	return
}

func createPreparedTask(ctx context.Context, project *models.Project, prepared *preparedTask) (*models.TaskJob, error) {
	priority, _ := new(big.Int).SetString(prepared.priority, 10)
	fee, _ := new(big.Int).SetString(prepared.taskFeeWei, 10)
	credits, _ := new(big.Int).SetString(prepared.credits, 10)
	return service.CreateRawTaskJob(ctx, config.GetDB(), service.CreateRawTaskJobInput{
		Project: project, TaskType: *prepared.request.TaskType, Model: prepared.model,
		TaskArgsJSON: prepared.taskArgs, TaskVersion: prepared.request.TaskVersion,
		MinVram: prepared.minVram, RequiredGPU: prepared.request.RequiredGPU,
		RequiredGPUVram: prepared.request.RequiredGPUVram,
		PriorityGwei: priority, BilledVram: prepared.billedVram, TaskFeeWei: fee,
		LLMBilling: prepared.llmBilling, ImageCredits: credits,
	})
}

func loadAccount(ctx context.Context, userID uint) (*models.CreditAccount, error) {
	var account models.CreditAccount
	err := config.GetDB().WithContext(ctx).Where("user_id = ?", userID).First(&account).Error
	return &account, err
}

func loadProjectTask(ctx context.Context, projectID, id uint) (*models.TaskJob, error) {
	var job models.TaskJob
	err := config.GetDB().WithContext(ctx).Where("project_id = ? AND id = ?", projectID, id).First(&job).Error
	return &job, err
}

func taskView(job *models.TaskJob) TaskView {
	status := "running"
	if job.Status == models.TaskJobStatusCompleted {
		status = "success"
	} else if job.Status == models.TaskJobStatusFailed {
		status = "failed"
	}
	view := TaskView{ID: job.ID, TaskType: job.TaskType, Status: status, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt}
	if status == "failed" && job.ErrorMessage != nil {
		view.ErrorMessage = *job.ErrorMessage
	}
	return view
}

func recordFailure(ctx context.Context, project *models.Project, model string, billedVram uint64) {
	if project == nil {
		return
	}
	priority, err := service.ResolveEffectivePriorityGwei(project)
	if err != nil {
		priority = &project.PriorityGwei.Int
	}
	now := time.Now()
	if _, err := service.ProcessTaskCall(ctx, config.GetDB(), service.RecordTaskCallInput{
		UserID: project.UserID, ProjectID: project.ID, TaskType: models.TaskTypeLLM,
		APIType: models.TaskAPITypeRaw, Model: model, PriorityGwei: priority,
		TokenUsageApplicable: false, Status: models.TaskCallStatusFailed,
		Credits: big.NewInt(0), AcceptedAt: now, CompletedAt: now,
		BilledVram: billedVram, Charge: false,
	}); err != nil {
		log.Errorf("Record raw task failure for project %d failed: %v", project.ID, err)
	}
}

func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": gin.H{"message": message, "type": "invalid_request_error"}})
}

func writeInternalError(c *gin.Context, err error) {
	log.Errorf("Raw task API failed: %v", err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "internal server error", "type": "server_error"}})
}
