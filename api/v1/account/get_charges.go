package account

import (
	"context"
	"crynux_as/api/v1/middleware"
	"crynux_as/api/v1/response"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
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

type chargeEventRow struct {
	ID        uint
	Amount    models.BigInt
	RefID     uint
	CreatedAt time.Time
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

	var events []chargeEventRow
	if err := baseEvents.Session(&gorm.Session{}).
		Select("id, amount, ref_id, created_at").
		Order("id DESC").
		Offset(offset).
		Limit(limit).
		Scan(&events).Error; err != nil {
		log.Errorf("Error listing charge events for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	if len(events) == 0 {
		return &GetChargesResponse{
			Data: &GetChargesData{
				Charges: []ChargeData{},
				Total:   total,
			},
		}, nil
	}

	refIDs := make([]uint, 0, len(events))
	for _, event := range events {
		refIDs = append(refIDs, event.RefID)
	}

	var records []models.LLMCallRecord
	if err := db.WithContext(dbCtx).
		Where("id IN ?", refIDs).
		Find(&records).Error; err != nil {
		log.Errorf("Error loading call records for charges of user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}
	recordByID := make(map[uint]models.LLMCallRecord, len(records))
	for _, record := range records {
		recordByID[record.ID] = record
	}

	charges := make([]ChargeData, 0, len(events))
	for _, event := range events {
		record, ok := recordByID[event.RefID]
		if !ok {
			log.Errorf("missing llm call record %d for charge event %d", event.RefID, event.ID)
			return nil, response.NewExceptionResponse(errors.New("charge call record missing"))
		}
		amount := models.BigInt{Int: *new(big.Int).Set(&event.Amount.Int)}
		charges = append(charges, ChargeData{
			ID:               record.ID,
			CreatedAt:        record.CreatedAt,
			ProjectID:        record.ProjectID,
			Model:            record.Model,
			PromptTokens:     record.PromptTokens,
			CompletionTokens: record.CompletionTokens,
			TotalTokens:      record.TotalTokens,
			TokenRatio:       service.DisplayTokenRatio(record.TokenRatio),
			Credits:          amount,
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
