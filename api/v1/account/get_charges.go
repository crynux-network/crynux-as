package account

import (
	"context"
	"crynux_as/api/v1/middleware"
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

type GetChargesInput struct {
	Offset int `query:"offset" description:"The offset of the charge records"`
	Limit  int `query:"limit" description:"The maximum number of the charge records to return"`
}

type ChargeData struct {
	ID               uint                 `json:"id" description:"The LLM call record ID"`
	CreatedAt        time.Time            `json:"created_at" description:"When the call was recorded"`
	ProjectID        uint                 `json:"project_id" description:"The project that made the call"`
	ProjectName      string               `json:"project_name" description:"The current project name"`
	Model            string               `json:"model" description:"The model ID used for the call"`
	PromptTokens     uint64               `json:"prompt_tokens" description:"Billed prompt token count"`
	CompletionTokens uint64               `json:"completion_tokens" description:"Billed completion token count"`
	TotalTokens      uint64               `json:"total_tokens" description:"Billed total token count"`
	TokenRatio       float64              `json:"token_ratio" description:"Project cost level (token ratio) used for the charge"`
	Credits          models.BigInt        `json:"credits" description:"Credits charged for the call"`
	BilledVram       uint64               `json:"billed_vram" description:"Effective VRAM in GB used for billing"`
	DurationMs       uint64               `json:"duration_ms" description:"Call duration in milliseconds"`
	Status           models.LLMCallStatus `json:"status" description:"Call status (success or failed)"`
}

type GetChargesData struct {
	Charges []ChargeData `json:"charges" description:"The charge records of the account"`
	Total   int64        `json:"total" description:"The total number of the charge records"`
}

type GetChargesResponse struct {
	response.Response
	Data *GetChargesData `json:"data"`
}

type chargeRow struct {
	ID               uint
	CreatedAt        time.Time
	ProjectID        uint
	ProjectName      sql.NullString
	Model            string
	PromptTokens     uint64
	CompletionTokens uint64
	TotalTokens      uint64
	TokenRatio       uint
	Credits          models.BigInt
	BilledVram       uint64
	DurationMs       uint64
	Status           models.LLMCallStatus
}

func GetCharges(c *gin.Context, in *GetChargesInput) (*GetChargesResponse, error) {
	address := middleware.GetUserAddress(c)
	if address == "" {
		return nil, response.NewValidationErrorResponse("Authorization", "Invalid token")
	}

	offset := in.Offset
	if offset < 0 {
		offset = 0
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	db := config.GetDB()
	ctx := c.Request.Context()

	user, err := models.FindUserByAddress(ctx, db, address)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewValidationErrorResponse("address", "User not found")
		}
		log.Errorf("Error loading user for charges: %v", err)
		return nil, response.NewExceptionResponse(err)
	}

	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	baseEvents := db.WithContext(dbCtx).
		Table("credit_events").
		Where("user_id = ?", user.ID).
		Where("type = ?", models.CreditEventTypeLLMCharge).
		Where("status = ?", models.CreditEventStatusProcessed)

	var total int64
	if err := baseEvents.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		log.Errorf("Error counting charges for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	if total == 0 {
		return &GetChargesResponse{
			Data: &GetChargesData{
				Charges: []ChargeData{},
				Total:   0,
			},
		}, nil
	}

	const query = `
SELECT
	r.id AS id,
	r.created_at AS created_at,
	r.project_id AS project_id,
	p.name AS project_name,
	r.model AS model,
	r.prompt_tokens AS prompt_tokens,
	r.completion_tokens AS completion_tokens,
	r.total_tokens AS total_tokens,
	r.token_ratio AS token_ratio,
	e.amount AS credits,
	r.billed_vram AS billed_vram,
	r.duration_ms AS duration_ms,
	r.status AS status
FROM (
	SELECT id, amount, ref_id, created_at
	FROM credit_events FORCE INDEX (idx_credit_events_user_type_status_id)
	WHERE user_id = ?
		AND type = ?
		AND status = ?
	ORDER BY id DESC
	LIMIT ? OFFSET ?
) AS e
JOIN llm_call_records AS r ON r.id = e.ref_id
LEFT JOIN projects AS p ON p.id = r.project_id
ORDER BY e.id DESC
`

	var rows []chargeRow
	if err := db.WithContext(dbCtx).Raw(
		query,
		user.ID,
		models.CreditEventTypeLLMCharge,
		models.CreditEventStatusProcessed,
		limit,
		offset,
	).Scan(&rows).Error; err != nil {
		log.Errorf("Error listing charges for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	charges := make([]ChargeData, 0, len(rows))
	for _, row := range rows {
		amount := models.BigInt{Int: *new(big.Int).Set(&row.Credits.Int)}
		projectName := ""
		if row.ProjectName.Valid {
			projectName = row.ProjectName.String
		}
		charges = append(charges, ChargeData{
			ID:               row.ID,
			CreatedAt:        row.CreatedAt,
			ProjectID:        row.ProjectID,
			ProjectName:      projectName,
			Model:            row.Model,
			PromptTokens:     row.PromptTokens,
			CompletionTokens: row.CompletionTokens,
			TotalTokens:      row.TotalTokens,
			TokenRatio:       service.DisplayTokenRatio(row.TokenRatio),
			Credits:          amount,
			BilledVram:       row.BilledVram,
			DurationMs:       row.DurationMs,
			Status:           row.Status,
		})
	}

	return &GetChargesResponse{
		Data: &GetChargesData{
			Charges: charges,
			Total:   total,
		},
	}, nil
}
