package account

import (
	"context"
	"crynux_as/api/v1/middleware"
	"crynux_as/api/v1/response"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"errors"
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

	scoped := func(tx *gorm.DB) *gorm.DB {
		return tx.
			Table("llm_call_records").
			Joins("JOIN projects ON projects.id = llm_call_records.project_id").
			Where("(llm_call_records.user_id = ? OR (llm_call_records.user_id = 0 AND projects.user_id = ?))", user.ID, user.ID).
			Where("llm_call_records.credits <> ?", "0")
	}

	var total int64
	if err := scoped(db.WithContext(dbCtx)).Count(&total).Error; err != nil {
		log.Errorf("Error counting charges for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	var records []models.LLMCallRecord
	if err := scoped(db.WithContext(dbCtx)).
		Select("llm_call_records.*").
		Order("llm_call_records.id DESC").
		Offset(offset).
		Limit(limit).
		Find(&records).Error; err != nil {
		log.Errorf("Error listing charges for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	charges := make([]ChargeData, 0, len(records))
	for _, record := range records {
		charges = append(charges, ChargeData{
			ID:               record.ID,
			CreatedAt:        record.CreatedAt,
			ProjectID:        record.ProjectID,
			Model:            record.Model,
			PromptTokens:     record.PromptTokens,
			CompletionTokens: record.CompletionTokens,
			TotalTokens:      record.TotalTokens,
			TokenRatio:       service.DisplayTokenRatio(record.TokenRatio),
			Credits:          record.Credits,
			BilledVram:       record.BilledVram,
			DurationMs:       record.DurationMs,
			Status:           record.Status,
		})
	}

	return &GetChargesResponse{
		Data: &GetChargesData{
			Charges: charges,
			Total:   total,
		},
	}, nil
}
