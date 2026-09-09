package projects

import (
	"context"
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

type GetProjectRequestsInput struct {
	ProjectID uint `path:"project_id" validate:"required" description:"The project ID"`
}

type ProjectRequestData struct {
	ID               uint                 `json:"id" description:"The LLM call record ID"`
	CreatedAt        time.Time            `json:"created_at" description:"When the call was recorded"`
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

type GetProjectRequestsData struct {
	Requests []ProjectRequestData `json:"requests" description:"The recent LLM call records of the project"`
}

type GetProjectRequestsResponse struct {
	response.Response
	Data *GetProjectRequestsData `json:"data"`
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

	var records []models.LLMCallRecord
	if err := db.WithContext(dbCtx).
		Where("project_id = ?", in.ProjectID).
		Order("id DESC").
		Limit(limit).
		Find(&records).Error; err != nil {
		log.Errorf("Error listing requests for project %d: %v", in.ProjectID, err)
		return nil, response.NewExceptionResponse(err)
	}

	requests := make([]ProjectRequestData, 0, len(records))
	for _, record := range records {
		requests = append(requests, ProjectRequestData{
			ID:               record.ID,
			CreatedAt:        record.CreatedAt,
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

	return &GetProjectRequestsResponse{
		Data: &GetProjectRequestsData{
			Requests: requests,
		},
	}, nil
}
