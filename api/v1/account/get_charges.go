package account

import (
	"context"
	"crynux_as/api/v1/middleware"
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

type GetChargesInput struct {
	Offset int `query:"offset" description:"The offset of the charge records"`
	Limit  int `query:"limit" description:"The maximum number of the charge records to return"`
}

type ChargeData struct {
	ID                 uint                  `json:"id" description:"The task call record ID"`
	CreatedAt          time.Time             `json:"created_at" description:"When the call was recorded"`
	ProjectID          uint                  `json:"project_id" description:"The project that made the call"`
	ProjectName        string                `json:"project_name" description:"The current project name"`
	Model              string                `json:"model" description:"The model ID used for the call"`
	TaskType           string                `json:"task_type" description:"llm, image, or video"`
	QuantizationConfig any                   `json:"quantization_config" description:"Selected quantization_config when known"`
	Components         any                   `json:"components" description:"Image or Video component replacements when known"`
	PromptTokens       *uint64               `json:"prompt_tokens" description:"Billed prompt token count; null for Image and Video tasks"`
	CompletionTokens   *uint64               `json:"completion_tokens" description:"Billed completion token count; null for Image and Video tasks"`
	TotalTokens        *uint64               `json:"total_tokens" description:"Billed total token count; null for Image and Video tasks"`
	ImageWidth         *uint64               `json:"image_width" description:"Image width for Image tasks; null for other task types"`
	ImageHeight        *uint64               `json:"image_height" description:"Image height for Image tasks; null for other task types"`
	ImageSteps         *uint64               `json:"image_steps" description:"Diffusion steps for Image tasks; null for other task types"`
	ImageCount         *uint64               `json:"image_count" description:"Generated image count for Image tasks; null for other task types"`
	VideoWidth         *uint64               `json:"video_width" description:"Billed video width for Video tasks; null for other task types"`
	VideoHeight        *uint64               `json:"video_height" description:"Billed video height for Video tasks; null for other task types"`
	VideoNumFrames     *uint64               `json:"video_num_frames" description:"Billed frame count for Video tasks; null for other task types"`
	VideoSteps         *uint64               `json:"video_steps" description:"Diffusion steps for Video tasks; null for other task types"`
	VideoCount         *uint64               `json:"video_count" description:"Generated video count for Video tasks; null for other task types"`
	PriorityGwei       string                `json:"priority_gwei" description:"Project Cost Level priority in Gwei used for the charge"`
	Credits            models.BigInt         `json:"credits" description:"Credits charged for the call"`
	BilledVram         uint64                `json:"billed_vram" description:"Effective VRAM in GB used for billing"`
	DurationMs         uint64                `json:"duration_ms" description:"Call duration in milliseconds"`
	Status             models.TaskCallStatus `json:"status" description:"Call status (success or failed)"`
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
	ID                   uint
	CreatedAt            time.Time
	ProjectID            uint
	ProjectName          sql.NullString
	Model                string
	TaskType             models.TaskType
	TokenUsageApplicable int8
	QuantizationConfig   []byte
	Components           []byte
	PromptTokens         uint64
	CompletionTokens     uint64
	TotalTokens          uint64
	ImageWidth           uint64
	ImageHeight          uint64
	ImageSteps           uint64
	ImageCount           uint64
	VideoWidth           uint64
	VideoHeight          uint64
	VideoNumFrames       uint64
	VideoSteps           uint64
	VideoCount           uint64
	PriorityGwei         string
	Credits              models.BigInt
	BilledVram           uint64
	DurationMs           uint64
	Status               models.TaskCallStatus
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
		Where("type = ?", models.CreditEventTypeTaskCharge).
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
	r.task_type AS task_type,
	r.token_usage_applicable AS token_usage_applicable,
	r.quantization_config AS quantization_config,
	r.components AS components,
	r.prompt_tokens AS prompt_tokens,
	r.completion_tokens AS completion_tokens,
	r.total_tokens AS total_tokens,
	r.image_width AS image_width,
	r.image_height AS image_height,
	r.image_steps AS image_steps,
	r.image_count AS image_count,
	r.video_width AS video_width,
	r.video_height AS video_height,
	r.video_num_frames AS video_num_frames,
	r.video_steps AS video_steps,
	r.video_count AS video_count,
	r.priority_gwei AS priority_gwei,
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
JOIN task_call_records AS r ON r.id = e.ref_id
LEFT JOIN projects AS p ON p.id = r.project_id
ORDER BY e.id DESC
`

	var rows []chargeRow
	if err := db.WithContext(dbCtx).Raw(
		query,
		user.ID,
		models.CreditEventTypeTaskCharge,
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
		item := ChargeData{
			ID:                 row.ID,
			CreatedAt:          row.CreatedAt,
			ProjectID:          row.ProjectID,
			ProjectName:        projectName,
			Model:              row.Model,
			TaskType:           service.TaskTypeAPIString(row.TaskType),
			QuantizationConfig: service.QuantizationConfigForAPI(json.RawMessage(row.QuantizationConfig)),
			PriorityGwei:       row.PriorityGwei,
			Credits:            amount,
			BilledVram:         row.BilledVram,
			DurationMs:         row.DurationMs,
			Status:             row.Status,
		}
		if len(row.Components) > 0 {
			var components any
			if err := json.Unmarshal(row.Components, &components); err == nil {
				item.Components = components
			}
		}
		switch {
		case row.TaskType == models.TaskTypeImage:
			item.ImageWidth = service.UintPtrIfPositive(row.ImageWidth)
			item.ImageHeight = service.UintPtrIfPositive(row.ImageHeight)
			item.ImageSteps = service.UintPtrIfPositive(row.ImageSteps)
			item.ImageCount = service.UintPtrIfPositive(row.ImageCount)
		case row.TaskType == models.TaskTypeVideo:
			item.VideoWidth = service.UintPtrIfPositive(row.VideoWidth)
			item.VideoHeight = service.UintPtrIfPositive(row.VideoHeight)
			item.VideoNumFrames = service.UintPtrIfPositive(row.VideoNumFrames)
			item.VideoSteps = service.UintPtrIfPositive(row.VideoSteps)
			item.VideoCount = service.UintPtrIfPositive(row.VideoCount)
		case row.TokenUsageApplicable != 0:
			p := row.PromptTokens
			cTok := row.CompletionTokens
			t := row.TotalTokens
			item.PromptTokens = &p
			item.CompletionTokens = &cTok
			item.TotalTokens = &t
		}
		charges = append(charges, item)
	}

	return &GetChargesResponse{
		Data: &GetChargesData{
			Charges: charges,
			Total:   total,
		},
	}, nil
}
