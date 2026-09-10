package projects

import (
	"context"
	"crynux_as/api/v1/response"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"database/sql"
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
	ID               uint            `json:"id" description:"Numeric ID from the source table"`
	Source           string          `json:"source" description:"job or call_record"`
	CreatedAt        time.Time       `json:"created_at" description:"When the request was accepted"`
	Model            string          `json:"model" description:"The model ID used for the call"`
	PromptTokens     *uint64         `json:"prompt_tokens" description:"Billed prompt token count, null while in progress"`
	CompletionTokens *uint64         `json:"completion_tokens" description:"Billed completion token count, null while in progress"`
	TotalTokens      *uint64         `json:"total_tokens" description:"Billed total token count, null while in progress"`
	TokenRatio       float64         `json:"token_ratio" description:"Project cost level (token ratio)"`
	Credits          *models.BigInt  `json:"credits" description:"Credits charged, null while in progress"`
	BilledVram       uint64          `json:"billed_vram" description:"Effective VRAM in GB used for billing"`
	DurationMs       *uint64         `json:"duration_ms" description:"Call duration in milliseconds, null while in progress"`
	Status           string          `json:"status" description:"queued, in_progress, success, or failed"`
}

type GetProjectRequestsData struct {
	Requests []ProjectRequestData `json:"requests" description:"Recent in-progress and finished LLM requests"`
}

type GetProjectRequestsResponse struct {
	response.Response
	Data *GetProjectRequestsData `json:"data"`
}

type projectRequestRow struct {
	ID               uint
	Source           string
	CreatedAt        time.Time
	Model            string
	PromptTokens     sql.NullInt64
	CompletionTokens sql.NullInt64
	TotalTokens      sql.NullInt64
	TokenRatio       uint
	Credits          sql.NullString
	BilledVram       uint64
	DurationMs       sql.NullInt64
	Status           string
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
	token_ratio,
	credits,
	billed_vram,
	duration_ms,
	status
FROM (
	SELECT
		j.id AS id,
		'job' AS source,
		j.created_at AS created_at,
		j.model AS model,
		CAST(NULL AS SIGNED) AS prompt_tokens,
		CAST(NULL AS SIGNED) AS completion_tokens,
		CAST(NULL AS SIGNED) AS total_tokens,
		j.token_ratio AS token_ratio,
		CAST(NULL AS CHAR) AS credits,
		j.billed_vram AS billed_vram,
		CAST(NULL AS SIGNED) AS duration_ms,
		CASE
			WHEN j.status = ? THEN 'queued'
			ELSE 'in_progress'
		END AS status
	FROM (
		SELECT id, created_at, model, token_ratio, billed_vram, status
		FROM llm_jobs
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
		r.token_ratio AS token_ratio,
		COALESCE(e.amount, '0') AS credits,
		r.billed_vram AS billed_vram,
		r.duration_ms AS duration_ms,
		CASE
			WHEN r.status = ? THEN 'success'
			ELSE 'failed'
		END AS status
	FROM (
		SELECT
			id,
			accepted_at,
			model,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			token_ratio,
			billed_vram,
			duration_ms,
			status
		FROM llm_call_records
		WHERE project_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	) AS r
	LEFT JOIN credit_events AS e
		ON e.type = ?
		AND e.status = ?
		AND e.ref_id = r.id
) AS combined
ORDER BY created_at DESC, source DESC, id DESC
LIMIT ?
`

	var rows []projectRequestRow
	if err := db.WithContext(dbCtx).Raw(
		query,
		models.LLMJobStatusPendingSubmit,
		in.ProjectID,
		models.LLMJobStatusPendingSubmit,
		models.LLMJobStatusSubmitted,
		models.LLMJobStatusInProgress,
		limit,
		models.LLMCallStatusSuccess,
		in.ProjectID,
		limit,
		models.CreditEventTypeLLMCharge,
		models.CreditEventStatusProcessed,
		limit,
	).Scan(&rows).Error; err != nil {
		log.Errorf("Error listing requests for project %d: %v", in.ProjectID, err)
		return nil, response.NewExceptionResponse(err)
	}

	requests := make([]ProjectRequestData, 0, len(rows))
	for _, row := range rows {
		item := ProjectRequestData{
			ID:         row.ID,
			Source:     row.Source,
			CreatedAt:  row.CreatedAt,
			Model:      row.Model,
			TokenRatio: service.DisplayTokenRatio(row.TokenRatio),
			BilledVram: row.BilledVram,
			Status:     row.Status,
		}
		if row.Source == "call_record" {
			promptTokens := uint64(row.PromptTokens.Int64)
			completionTokens := uint64(row.CompletionTokens.Int64)
			totalTokens := uint64(row.TotalTokens.Int64)
			durationMs := uint64(row.DurationMs.Int64)
			item.PromptTokens = &promptTokens
			item.CompletionTokens = &completionTokens
			item.TotalTokens = &totalTokens
			item.DurationMs = &durationMs
			creditsValue := big.NewInt(0)
			if row.Credits.Valid && row.Credits.String != "" {
				if parsed, ok := new(big.Int).SetString(row.Credits.String, 10); ok {
					creditsValue = parsed
				}
			}
			credits := models.BigInt{Int: *creditsValue}
			item.Credits = &credits
		}
		requests = append(requests, item)
	}

	return &GetProjectRequestsResponse{
		Data: &GetProjectRequestsData{
			Requests: requests,
		},
	}, nil
}
