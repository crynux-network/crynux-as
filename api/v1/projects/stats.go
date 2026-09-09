package projects

import (
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

type GetProjectStatsInput struct {
	ProjectID uint   `path:"project_id" validate:"required" description:"The project ID"`
	Range     string `query:"range" validate:"required" description:"Stats range: 1d or 1m"`
}

type ProjectStatsCountPoint struct {
	Timestamp    int64  `json:"timestamp"`
	SuccessCount uint64 `json:"success_count"`
	FailureCount uint64 `json:"failure_count"`
	Complete     bool   `json:"complete"`
}

type ProjectStatsCreditsPoint struct {
	Timestamp int64  `json:"timestamp"`
	Value     string `json:"value"`
	Complete  bool   `json:"complete"`
}

type ProjectStatsTokenPoint struct {
	Timestamp        int64  `json:"timestamp"`
	PromptTokens     uint64 `json:"prompt_tokens"`
	CompletionTokens uint64 `json:"completion_tokens"`
	Complete         bool   `json:"complete"`
}

type GetProjectStatsData struct {
	Range         string                     `json:"range"`
	RequestSeries []ProjectStatsCountPoint   `json:"request_series"`
	CreditsSeries []ProjectStatsCreditsPoint `json:"credits_series"`
	TokenSeries   []ProjectStatsTokenPoint   `json:"token_series"`
}

type GetProjectStatsResponse struct {
	response.Response
	Data *GetProjectStatsData `json:"data"`
}

func GetProjectStats(c *gin.Context, in *GetProjectStatsInput) (*GetProjectStatsResponse, error) {
	user, err := currentUser(c)
	if err != nil {
		return nil, err
	}

	rangeType := service.ProjectStatsRange(in.Range)
	switch rangeType {
	case service.ProjectStatsRange1d, service.ProjectStatsRange1m:
	default:
		return nil, response.NewValidationErrorResponse("range", "must be 1d or 1m")
	}

	db := config.GetDB()
	if _, err := findOwnedProject(c.Request.Context(), db, user.ID, in.ProjectID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewNotFoundErrorResponse()
		}
		log.Errorf("Error loading project %d for stats: %v", in.ProjectID, err)
		return nil, response.NewExceptionResponse(err)
	}

	stats, err := service.GetProjectUsageStats(c.Request.Context(), db, in.ProjectID, rangeType, time.Now())
	if err != nil {
		log.Errorf("Error loading project stats for project %d: %v", in.ProjectID, err)
		return nil, response.NewExceptionResponse(err)
	}

	requestSeries := make([]ProjectStatsCountPoint, 0, len(stats.RequestSeries))
	for _, point := range stats.RequestSeries {
		requestSeries = append(requestSeries, ProjectStatsCountPoint{
			Timestamp:    point.Timestamp,
			SuccessCount: point.SuccessCount,
			FailureCount: point.FailureCount,
			Complete:     point.Complete,
		})
	}
	creditsSeries := make([]ProjectStatsCreditsPoint, 0, len(stats.CreditsSeries))
	for _, point := range stats.CreditsSeries {
		creditsSeries = append(creditsSeries, ProjectStatsCreditsPoint{
			Timestamp: point.Timestamp,
			Value:     point.Value.String(),
			Complete:  point.Complete,
		})
	}
	tokenSeries := make([]ProjectStatsTokenPoint, 0, len(stats.TokenSeries))
	for _, point := range stats.TokenSeries {
		tokenSeries = append(tokenSeries, ProjectStatsTokenPoint{
			Timestamp:        point.Timestamp,
			PromptTokens:     point.PromptTokens,
			CompletionTokens: point.CompletionTokens,
			Complete:         point.Complete,
		})
	}

	return &GetProjectStatsResponse{
		Data: &GetProjectStatsData{
			Range:         string(stats.Range),
			RequestSeries: requestSeries,
			CreditsSeries: creditsSeries,
			TokenSeries:   tokenSeries,
		},
	}, nil
}

type GetProjectCompletionDurationStatsInput struct {
	ProjectID uint   `path:"project_id" validate:"required" description:"The project ID"`
	Range     string `query:"range" validate:"required" description:"Stats range: 1h, 1d, or 7d"`
}

type CompletionDurationBucketData struct {
	BucketIndex   uint    `json:"bucket_index"`
	BucketLabel   string  `json:"bucket_label"`
	MinDurationMs uint64  `json:"min_duration_ms"`
	MaxDurationMs *uint64 `json:"max_duration_ms"`
	RequestCount  uint64  `json:"request_count"`
}

type GetProjectCompletionDurationStatsData struct {
	Range       string                          `json:"range"`
	WindowStart int64                           `json:"window_start"`
	WindowEnd   int64                           `json:"window_end"`
	Buckets     []CompletionDurationBucketData  `json:"buckets"`
}

type GetProjectCompletionDurationStatsResponse struct {
	response.Response
	Data *GetProjectCompletionDurationStatsData `json:"data"`
}

func GetProjectCompletionDurationStats(c *gin.Context, in *GetProjectCompletionDurationStatsInput) (*GetProjectCompletionDurationStatsResponse, error) {
	user, err := currentUser(c)
	if err != nil {
		return nil, err
	}

	rangeType := models.UsageStatsRangeType(in.Range)
	switch rangeType {
	case models.UsageStatsRange1h, models.UsageStatsRange1d, models.UsageStatsRange7d:
	default:
		return nil, response.NewValidationErrorResponse("range", "must be 1h, 1d, or 7d")
	}

	db := config.GetDB()
	if _, err := findOwnedProject(c.Request.Context(), db, user.ID, in.ProjectID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewNotFoundErrorResponse()
		}
		log.Errorf("Error loading project %d for duration stats: %v", in.ProjectID, err)
		return nil, response.NewExceptionResponse(err)
	}

	rows, err := service.GetProjectDurationHistogramSnapshots(c.Request.Context(), db, in.ProjectID, rangeType)
	if err != nil {
		log.Errorf("Error loading duration stats for project %d: %v", in.ProjectID, err)
		return nil, response.NewExceptionResponse(err)
	}

	buckets := make([]CompletionDurationBucketData, 0, len(rows))
	var windowStart, windowEnd int64
	for _, row := range rows {
		if windowStart == 0 || row.WindowStart < windowStart {
			windowStart = row.WindowStart
		}
		if row.WindowEnd > windowEnd {
			windowEnd = row.WindowEnd
		}
		buckets = append(buckets, CompletionDurationBucketData{
			BucketIndex:   row.BucketIndex,
			BucketLabel:   row.BucketLabel,
			MinDurationMs: row.MinDurationMs,
			MaxDurationMs: row.MaxDurationMs,
			RequestCount:  row.RequestCount,
		})
	}

	return &GetProjectCompletionDurationStatsResponse{
		Data: &GetProjectCompletionDurationStatsData{
			Range:       string(rangeType),
			WindowStart: windowStart,
			WindowEnd:   windowEnd,
			Buckets:     buckets,
		},
	}, nil
}

type GetProjectModelStatsInput struct {
	ProjectID uint   `path:"project_id" validate:"required" description:"The project ID"`
	Range     string `query:"range" validate:"required" description:"Stats range: 1h, 1d, or 7d"`
}

type ProjectModelStatsItem struct {
	Rank             uint    `json:"rank"`
	Model            string  `json:"model"`
	RequestCount     uint64  `json:"request_count"`
	PromptTokens     uint64  `json:"prompt_tokens"`
	CompletionTokens uint64  `json:"completion_tokens"`
	SuccessRate      float64 `json:"success_rate"`
	Credits          string  `json:"credits"`
}

type GetProjectModelStatsData struct {
	Range       string                   `json:"range"`
	WindowStart int64                    `json:"window_start"`
	WindowEnd   int64                    `json:"window_end"`
	Models      []ProjectModelStatsItem  `json:"models"`
}

type GetProjectModelStatsResponse struct {
	response.Response
	Data *GetProjectModelStatsData `json:"data"`
}

func GetProjectModelStats(c *gin.Context, in *GetProjectModelStatsInput) (*GetProjectModelStatsResponse, error) {
	user, err := currentUser(c)
	if err != nil {
		return nil, err
	}

	rangeType := models.UsageStatsRangeType(in.Range)
	switch rangeType {
	case models.UsageStatsRange1h, models.UsageStatsRange1d, models.UsageStatsRange7d:
	default:
		return nil, response.NewValidationErrorResponse("range", "must be 1h, 1d, or 7d")
	}

	db := config.GetDB()
	if _, err := findOwnedProject(c.Request.Context(), db, user.ID, in.ProjectID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NewNotFoundErrorResponse()
		}
		log.Errorf("Error loading project %d for model stats: %v", in.ProjectID, err)
		return nil, response.NewExceptionResponse(err)
	}

	rows, err := service.GetProjectModelUsageSnapshots(c.Request.Context(), db, in.ProjectID, rangeType)
	if err != nil {
		log.Errorf("Error loading model stats for project %d: %v", in.ProjectID, err)
		return nil, response.NewExceptionResponse(err)
	}

	modelsData := make([]ProjectModelStatsItem, 0, len(rows))
	var windowStart, windowEnd int64
	for _, row := range rows {
		if windowStart == 0 || row.WindowStart < windowStart {
			windowStart = row.WindowStart
		}
		if row.WindowEnd > windowEnd {
			windowEnd = row.WindowEnd
		}
		successRate := 0.0
		if row.RequestCount > 0 {
			successRate = float64(row.SuccessCount) / float64(row.RequestCount)
		}
		modelsData = append(modelsData, ProjectModelStatsItem{
			Rank:             row.Rank,
			Model:            row.Model,
			RequestCount:     row.RequestCount,
			PromptTokens:     row.PromptTokens,
			CompletionTokens: row.CompletionTokens,
			SuccessRate:      successRate,
			Credits:          row.Credits.String(),
		})
	}

	return &GetProjectModelStatsResponse{
		Data: &GetProjectModelStatsData{
			Range:       string(rangeType),
			WindowStart: windowStart,
			WindowEnd:   windowEnd,
			Models:      modelsData,
		},
	}, nil
}
