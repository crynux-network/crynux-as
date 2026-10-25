package projects

import (
	"context"
	"crynux_as/api/v1/response"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type GetProjectRequestsInput struct {
	ProjectID uint `path:"project_id" validate:"required" description:"The project ID"`
}

type ProjectRequestData struct {
	ID                 uint            `json:"id" description:"Numeric ID from the source table"`
	Source             string          `json:"source" description:"job or call_record"`
	CreatedAt          time.Time       `json:"created_at" description:"When the request was accepted"`
	Model              string          `json:"model" description:"The model ID used for the call"`
	TaskType           string          `json:"task_type" description:"llm or image"`
	QuantizationConfig any             `json:"quantization_config" description:"Selected quantization_config when known"`
	Components         any             `json:"components" description:"Image component replacements when known"`
	PromptTokens       *uint64         `json:"prompt_tokens" description:"Billed prompt token count; null while in progress, for Image tasks, or when token usage does not apply"`
	CompletionTokens   *uint64         `json:"completion_tokens" description:"Billed completion token count; null while in progress, for Image tasks, or when token usage does not apply"`
	TotalTokens        *uint64         `json:"total_tokens" description:"Billed total token count; null while in progress, for Image tasks, or when token usage does not apply"`
	ImageWidth         *uint64         `json:"image_width" description:"Image width for Image tasks; null for LLM or when unknown"`
	ImageHeight        *uint64         `json:"image_height" description:"Image height for Image tasks; null for LLM or when unknown"`
	ImageSteps         *uint64         `json:"image_steps" description:"Diffusion steps for Image tasks; null for LLM or when unknown"`
	ImageCount         *uint64         `json:"image_count" description:"Generated image count for Image tasks; null for LLM or when unknown"`
	PriorityGwei       string          `json:"priority_gwei" description:"Project Cost Level priority in Gwei"`
	Credits            *models.BigInt  `json:"credits" description:"Credits charged, null while in progress"`
	BilledVram         uint64          `json:"billed_vram" description:"Effective VRAM in GB used for billing"`
	DurationMs         *uint64         `json:"duration_ms" description:"Call duration in milliseconds, null while in progress"`
	Status             string          `json:"status" description:"queued, in_progress, success, or failed"`
	Phase              string          `json:"phase" description:"Logical execution phase; unfinished jobs use Bridge/AS phase, finished rows use success or failed"`
	ErrorMessage       *string         `json:"error_message" description:"Failure reason when a failed call record still has a retained task_jobs row; otherwise null"`
}

type GetProjectRequestsData struct {
	Requests []ProjectRequestData `json:"requests" description:"Recent in-progress and finished task requests"`
}

type GetProjectRequestsResponse struct {
	response.Response
	Data *GetProjectRequestsData `json:"data"`
}

type projectRequestRow struct {
	ID                   uint
	Source               string
	CreatedAt            time.Time
	Model                string
	PromptTokens         sql.NullInt64
	CompletionTokens     sql.NullInt64
	TotalTokens          sql.NullInt64
	TokenUsageApplicable sql.NullInt64
	TaskType             sql.NullInt64
	TaskArgsJSON         sql.NullString
	ModelCacheHash       sql.NullString
	ExecutionDType       sql.NullString
	QuantizationConfig   []byte
	Components           []byte
	ImageWidth           sql.NullInt64
	ImageHeight          sql.NullInt64
	ImageSteps           sql.NullInt64
	ImageCount           sql.NullInt64
	PriorityGwei         string
	Credits              sql.NullString
	BilledVram           uint64
	DurationMs           sql.NullInt64
	Status               string
	Phase                string
	ErrorMessage         sql.NullString
}

func GetProjectRequests(c *gin.Context, in *GetProjectRequestsInput) (*GetProjectRequestsResponse, error) {
	user, err := currentUser(c)
	if err != nil {
		return nil, err
	}

	db := config.GetDB()
	if _, err := findOwnedProject(c.Request.Context(), db, user.ID, in.ProjectID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewNotFoundErrorResponse()
		}
		log.Errorf("Error loading project %d for requests: %v", in.ProjectID, err)
		return nil, response.NewExceptionResponse(err)
	}

	limit := int(config.GetConfig().LLM.ProjectRecentRequestsLimit)

	dbCtx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	query := `
SELECT
	id,
	source,
	created_at,
	model,
	prompt_tokens,
	completion_tokens,
	total_tokens,
	token_usage_applicable,
	task_type,
	task_args_json,
	model_cache_hash,
	execution_dtype,
	quantization_config,
	components,
	image_width,
	image_height,
	image_steps,
	image_count,
	priority_gwei,
	credits,
	billed_vram,
	duration_ms,
	status,
	phase,
	error_message
FROM (
	SELECT
		j.id AS id,
		'job' AS source,
		j.created_at AS created_at,
		j.model AS model,
		CAST(NULL AS SIGNED) AS prompt_tokens,
		CAST(NULL AS SIGNED) AS completion_tokens,
		CAST(NULL AS SIGNED) AS total_tokens,
		CAST(NULL AS SIGNED) AS token_usage_applicable,
		j.task_type AS task_type,
		j.task_args_json AS task_args_json,
		CAST(NULL AS CHAR) AS model_cache_hash,
		CAST(NULL AS CHAR) AS execution_dtype,
		CAST(NULL AS JSON) AS quantization_config,
		CAST(NULL AS JSON) AS components,
		CAST(NULL AS SIGNED) AS image_width,
		CAST(NULL AS SIGNED) AS image_height,
		CAST(NULL AS SIGNED) AS image_steps,
		CAST(NULL AS SIGNED) AS image_count,
		j.priority_gwei AS priority_gwei,
		CAST(NULL AS CHAR) AS credits,
		j.billed_vram AS billed_vram,
		CAST(NULL AS SIGNED) AS duration_ms,
		CASE
			WHEN j.status = ? THEN 'queued'
			ELSE 'in_progress'
		END AS status,
		COALESCE(NULLIF(j.phase, ''), 'queued') AS phase,
		CAST(NULL AS CHAR) AS error_message
	FROM (
		SELECT id, created_at, model, task_type, task_args_json, priority_gwei, billed_vram, status, phase
		FROM task_jobs
		WHERE project_id = ?
			AND status IN (?, ?, ?)
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	) AS j

	UNION ALL

	SELECT
		r.id AS id,
		'call_record' AS source,
		r.accepted_at AS created_at,
		r.model AS model,
		r.prompt_tokens AS prompt_tokens,
		r.completion_tokens AS completion_tokens,
		r.total_tokens AS total_tokens,
		r.token_usage_applicable AS token_usage_applicable,
		r.task_type AS task_type,
		CAST(NULL AS CHAR) AS task_args_json,
		r.model_cache_hash AS model_cache_hash,
		r.execution_dtype AS execution_dtype,
		r.quantization_config AS quantization_config,
		r.components AS components,
		r.image_width AS image_width,
		r.image_height AS image_height,
		r.image_steps AS image_steps,
		r.image_count AS image_count,
		r.priority_gwei AS priority_gwei,
		COALESCE(e.amount, '0') AS credits,
		r.billed_vram AS billed_vram,
		r.duration_ms AS duration_ms,
		CASE
			WHEN r.status = ? THEN 'success'
			ELSE 'failed'
		END AS status,
		CASE
			WHEN r.status = ? THEN 'success'
			ELSE 'failed'
		END AS phase,
		CASE
			WHEN r.status = ? THEN CAST(NULL AS CHAR)
			ELSE j.error_message
		END AS error_message
	FROM (
		SELECT
			id,
			accepted_at,
			model,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			token_usage_applicable,
			task_type,
			model_cache_hash,
			execution_dtype,
			quantization_config,
			components,
			image_width,
			image_height,
			image_steps,
			image_count,
			priority_gwei,
			billed_vram,
			duration_ms,
			status,
			task_job_id
		FROM task_call_records
		WHERE project_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	) AS r
	LEFT JOIN credit_events AS e
		ON e.type = ?
		AND e.status = ?
		AND e.ref_id = r.id
	LEFT JOIN task_jobs AS j
		ON j.id = r.task_job_id
) AS combined
ORDER BY created_at DESC, source DESC, id DESC
LIMIT ?
`

	var rows []projectRequestRow
	if err := db.WithContext(dbCtx).Raw(
		query,
		models.TaskJobStatusPendingSubmit,
		in.ProjectID,
		models.TaskJobStatusPendingSubmit,
		models.TaskJobStatusSubmitted,
		models.TaskJobStatusInProgress,
		limit,
		models.TaskCallStatusSuccess,
		models.TaskCallStatusSuccess,
		models.TaskCallStatusSuccess,
		in.ProjectID,
		limit,
		models.CreditEventTypeTaskCharge,
		models.CreditEventStatusProcessed,
		limit,
	).Scan(&rows).Error; err != nil {
		log.Errorf("Error listing requests for project %d: %v", in.ProjectID, err)
		return nil, response.NewExceptionResponse(err)
	}

	requests := make([]ProjectRequestData, 0, len(rows))
	for _, row := range rows {
		item := ProjectRequestData{
			ID:           row.ID,
			Source:       row.Source,
			CreatedAt:    row.CreatedAt,
			Model:        row.Model,
			PriorityGwei: row.PriorityGwei,
			BilledVram:   row.BilledVram,
			Status:       row.Status,
			Phase:        row.Phase,
			TaskType:     "llm",
		}
		if row.ErrorMessage.Valid && row.ErrorMessage.String != "" {
			msg := row.ErrorMessage.String
			item.ErrorMessage = &msg
		}
		taskType := models.TaskTypeLLM
		if row.TaskType.Valid {
			taskType = models.TaskType(row.TaskType.Int64)
			item.TaskType = service.TaskTypeAPIString(taskType)
		}

		if row.Source == "job" {
			workload := service.ExtractTaskCallWorkloadFields(taskType, row.Model, row.TaskArgsJSON.String)
			item.QuantizationConfig = service.QuantizationConfigForAPI(workload.QuantizationConfig)
			if len(workload.Components) > 0 {
				var components any
				if err := json.Unmarshal(workload.Components, &components); err == nil {
					item.Components = components
				}
			}
			if taskType == models.TaskTypeImage {
				item.ImageWidth = uintPtrIfPositive(workload.ImageWidth)
				item.ImageHeight = uintPtrIfPositive(workload.ImageHeight)
				item.ImageSteps = uintPtrIfPositive(workload.ImageSteps)
				item.ImageCount = uintPtrIfPositive(workload.ImageCount)
			}
			requests = append(requests, item)
			continue
		}

		item.QuantizationConfig = service.QuantizationConfigForAPI(json.RawMessage(row.QuantizationConfig))
		if len(row.Components) > 0 {
			var components any
			if err := json.Unmarshal(row.Components, &components); err == nil {
				item.Components = components
			}
		}

		durationMs := uint64(row.DurationMs.Int64)
		item.DurationMs = &durationMs
		creditsValue := big.NewInt(0)
		if row.Credits.Valid && row.Credits.String != "" {
			if parsed, ok := new(big.Int).SetString(row.Credits.String, 10); ok {
				creditsValue = parsed
			}
		}
		credits := models.BigInt{Int: *creditsValue}
		item.Credits = &credits
		tokenUsageApplicable := row.TokenUsageApplicable.Valid && row.TokenUsageApplicable.Int64 != 0
		if taskType == models.TaskTypeImage {
			tokenUsageApplicable = false
			item.ImageWidth = uintPtrIfPositive(uint64(row.ImageWidth.Int64))
			item.ImageHeight = uintPtrIfPositive(uint64(row.ImageHeight.Int64))
			item.ImageSteps = uintPtrIfPositive(uint64(row.ImageSteps.Int64))
			item.ImageCount = uintPtrIfPositive(uint64(row.ImageCount.Int64))
		}
		if tokenUsageApplicable {
			promptTokens := uint64(row.PromptTokens.Int64)
			completionTokens := uint64(row.CompletionTokens.Int64)
			totalTokens := uint64(row.TotalTokens.Int64)
			item.PromptTokens = &promptTokens
			item.CompletionTokens = &completionTokens
			item.TotalTokens = &totalTokens
		}
		requests = append(requests, item)
	}

	return &GetProjectRequestsResponse{
		Data: &GetProjectRequestsData{
			Requests: requests,
		},
	}, nil
}

func uintPtrIfPositive(v uint64) *uint64 {
	if v == 0 {
		return nil
	}
	return &v
}
