package account

import (
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

type GetAccountStatsInput struct {
	Range string `query:"range" validate:"required" description:"Stats range: 1d, 7d, or 1m"`
}

type AccountStatsSeriesPoint struct {
	Timestamp int64  `json:"timestamp"`
	Value     string `json:"value"`
	Complete  bool   `json:"complete"`
}

type AccountStatsCountPoint struct {
	Timestamp int64  `json:"timestamp"`
	Value     uint64 `json:"value"`
	Complete  bool   `json:"complete"`
}

type GetAccountStatsData struct {
	Range            string                   `json:"range"`
	RequestCount     uint64                   `json:"request_count"`
	PromptTokens     uint64                   `json:"prompt_tokens"`
	CompletionTokens uint64                   `json:"completion_tokens"`
	Credits          string                   `json:"credits"`
	SuccessRate      float64                  `json:"success_rate"`
	RequestsSeries   []AccountStatsCountPoint `json:"requests_series"`
	CreditsSeries    []AccountStatsSeriesPoint `json:"credits_series"`
}

type GetAccountStatsResponse struct {
	response.Response
	Data *GetAccountStatsData `json:"data"`
}

func GetAccountStats(c *gin.Context, in *GetAccountStatsInput) (*GetAccountStatsResponse, error) {
	address := middleware.GetUserAddress(c)
	if address == "" {
		return nil, response.NewValidationErrorResponse("Authorization", "Invalid token")
	}

	rangeType := service.AccountStatsRange(in.Range)
	switch rangeType {
	case service.AccountStatsRange1d, service.AccountStatsRange7d, service.AccountStatsRange1m:
	default:
		return nil, response.NewValidationErrorResponse("range", "must be 1d, 7d, or 1m")
	}

	db := config.GetDB()
	user, err := models.FindUserByAddress(c.Request.Context(), db, address)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewValidationErrorResponse("address", "User not found")
		}
		log.Errorf("Error loading user for account stats: %v", err)
		return nil, response.NewExceptionResponse(err)
	}

	stats, err := service.GetAccountUsageStats(c.Request.Context(), db, user.ID, rangeType, time.Now())
	if err != nil {
		log.Errorf("Error loading account stats for user %d: %v", user.ID, err)
		return nil, response.NewExceptionResponse(err)
	}

	requestsSeries := make([]AccountStatsCountPoint, 0, len(stats.RequestsSeries))
	for _, point := range stats.RequestsSeries {
		requestsSeries = append(requestsSeries, AccountStatsCountPoint{
			Timestamp: point.Timestamp,
			Value:     point.Value,
			Complete:  point.Complete,
		})
	}
	creditsSeries := make([]AccountStatsSeriesPoint, 0, len(stats.CreditsSeries))
	for _, point := range stats.CreditsSeries {
		creditsSeries = append(creditsSeries, AccountStatsSeriesPoint{
			Timestamp: point.Timestamp,
			Value:     point.Value.String(),
			Complete:  point.Complete,
		})
	}

	return &GetAccountStatsResponse{
		Data: &GetAccountStatsData{
			Range:            string(stats.Range),
			RequestCount:     stats.RequestCount,
			PromptTokens:     stats.PromptTokens,
			CompletionTokens: stats.CompletionTokens,
			Credits:          stats.Credits.String(),
			SuccessRate:      stats.SuccessRate,
			RequestsSeries:   requestsSeries,
			CreditsSeries:    creditsSeries,
		},
	}, nil
}
