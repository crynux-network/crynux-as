package service

import (
	"context"
	"crynux_as/models"
	"errors"
	"math/big"
	"time"

	"gorm.io/gorm"
)

type AccountStatsRange string

const (
	AccountStatsRange1d AccountStatsRange = "1d"
	AccountStatsRange7d AccountStatsRange = "7d"
	AccountStatsRange1m AccountStatsRange = "1m"
)

type ProjectStatsRange string

const (
	ProjectStatsRange1d ProjectStatsRange = "1d"
	ProjectStatsRange1m ProjectStatsRange = "1m"
)

type UsageSeriesPoint struct {
	Timestamp int64
	Value     *big.Int
	Complete  bool
}

type UsageCountSeriesPoint struct {
	Timestamp int64
	Value     uint64
	Complete  bool
}

type UsageStackedCountPoint struct {
	Timestamp    int64
	SuccessCount uint64
	FailureCount uint64
	Complete     bool
}

type UsageStackedTokenPoint struct {
	Timestamp        int64
	PromptTokens     uint64
	CompletionTokens uint64
	Complete         bool
}

type AccountUsageStats struct {
	Range            AccountStatsRange
	RequestCount     uint64
	PromptTokens     uint64
	CompletionTokens uint64
	Credits          *big.Int
	SuccessCount     uint64
	FailureCount     uint64
	SuccessRate      float64
	RequestsSeries   []UsageCountSeriesPoint
	CreditsSeries    []UsageSeriesPoint
}

type ProjectUsageStats struct {
	Range          ProjectStatsRange
	RequestSeries  []UsageStackedCountPoint
	CreditsSeries  []UsageSeriesPoint
	TokenSeries    []UsageStackedTokenPoint
}

func GetAccountUsageStats(ctx context.Context, db *gorm.DB, userID uint, rangeType AccountStatsRange, now time.Time) (*AccountUsageStats, error) {
	starts, completeFlags, err := accountHourlyPeriodStarts(rangeType, now.Unix())
	if err != nil {
		return nil, err
	}
	if len(starts) == 0 {
		return nil, errors.New("empty period starts")
	}

	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var rows []models.AccountUsageHourlyStat
	if err := db.WithContext(dbCtx).
		Where("user_id = ? AND period_start >= ? AND period_start <= ?", userID, starts[0], starts[len(starts)-1]).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	byPeriod := make(map[int64]models.AccountUsageHourlyStat, len(rows))
	for _, row := range rows {
		byPeriod[row.PeriodStart] = row
	}

	out := &AccountUsageStats{
		Range:         rangeType,
		Credits:       big.NewInt(0),
		RequestsSeries: make([]UsageCountSeriesPoint, 0, len(starts)),
		CreditsSeries:  make([]UsageSeriesPoint, 0, len(starts)),
	}

	if rangeType == AccountStatsRange1d {
		for i, start := range starts {
			row := byPeriod[start]
			out.RequestCount += row.RequestCount
			out.SuccessCount += row.SuccessCount
			out.FailureCount += row.FailureCount
			out.PromptTokens += row.PromptTokens
			out.CompletionTokens += row.CompletionTokens
			out.Credits.Add(out.Credits, &row.Credits.Int)
			out.RequestsSeries = append(out.RequestsSeries, UsageCountSeriesPoint{
				Timestamp: start,
				Value:     row.RequestCount,
				Complete:  completeFlags[i],
			})
			out.CreditsSeries = append(out.CreditsSeries, UsageSeriesPoint{
				Timestamp: start,
				Value:     new(big.Int).Set(&row.Credits.Int),
				Complete:  completeFlags[i],
			})
		}
	} else {
		dayMap := make(map[int64]*models.AccountUsageHourlyStat)
		dayComplete := make(map[int64]bool)
		orderedDays := make([]int64, 0)
		for i, start := range starts {
			day := UnixDayStart(start)
			agg, ok := dayMap[day]
			if !ok {
				agg = &models.AccountUsageHourlyStat{PeriodStart: day, Credits: models.BigInt{Int: *big.NewInt(0)}}
				dayMap[day] = agg
				orderedDays = append(orderedDays, day)
				dayComplete[day] = completeFlags[i]
			}
			row := byPeriod[start]
			agg.RequestCount += row.RequestCount
			agg.SuccessCount += row.SuccessCount
			agg.FailureCount += row.FailureCount
			agg.PromptTokens += row.PromptTokens
			agg.CompletionTokens += row.CompletionTokens
			agg.Credits.Int.Add(&agg.Credits.Int, &row.Credits.Int)
			if !completeFlags[i] {
				dayComplete[day] = false
			}
		}
		for _, day := range orderedDays {
			agg := dayMap[day]
			out.RequestCount += agg.RequestCount
			out.SuccessCount += agg.SuccessCount
			out.FailureCount += agg.FailureCount
			out.PromptTokens += agg.PromptTokens
			out.CompletionTokens += agg.CompletionTokens
			out.Credits.Add(out.Credits, &agg.Credits.Int)
			out.RequestsSeries = append(out.RequestsSeries, UsageCountSeriesPoint{
				Timestamp: day,
				Value:     agg.RequestCount,
				Complete:  dayComplete[day],
			})
			out.CreditsSeries = append(out.CreditsSeries, UsageSeriesPoint{
				Timestamp: day,
				Value:     new(big.Int).Set(&agg.Credits.Int),
				Complete:  dayComplete[day],
			})
		}
	}

	if out.RequestCount > 0 {
		out.SuccessRate = float64(out.SuccessCount) / float64(out.RequestCount)
	}
	return out, nil
}

func GetProjectUsageStats(ctx context.Context, db *gorm.DB, projectID uint, rangeType ProjectStatsRange, now time.Time) (*ProjectUsageStats, error) {
	starts, completeFlags, err := projectHourlyPeriodStarts(rangeType, now.Unix())
	if err != nil {
		return nil, err
	}

	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var rows []models.ProjectUsageHourlyStat
	if err := db.WithContext(dbCtx).
		Where("project_id = ? AND period_start >= ? AND period_start <= ?", projectID, starts[0], starts[len(starts)-1]).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	byPeriod := make(map[int64]models.ProjectUsageHourlyStat, len(rows))
	for _, row := range rows {
		byPeriod[row.PeriodStart] = row
	}

	out := &ProjectUsageStats{
		Range:         rangeType,
		RequestSeries: make([]UsageStackedCountPoint, 0, len(starts)),
		CreditsSeries: make([]UsageSeriesPoint, 0, len(starts)),
		TokenSeries:   make([]UsageStackedTokenPoint, 0, len(starts)),
	}

	if rangeType == ProjectStatsRange1d {
		for i, start := range starts {
			row := byPeriod[start]
			out.RequestSeries = append(out.RequestSeries, UsageStackedCountPoint{
				Timestamp:    start,
				SuccessCount: row.SuccessCount,
				FailureCount: row.FailureCount,
				Complete:     completeFlags[i],
			})
			out.CreditsSeries = append(out.CreditsSeries, UsageSeriesPoint{
				Timestamp: start,
				Value:     new(big.Int).Set(&row.Credits.Int),
				Complete:  completeFlags[i],
			})
			out.TokenSeries = append(out.TokenSeries, UsageStackedTokenPoint{
				Timestamp:        start,
				PromptTokens:     row.PromptTokens,
				CompletionTokens: row.CompletionTokens,
				Complete:         completeFlags[i],
			})
		}
		return out, nil
	}

	dayMapReq := make(map[int64]*UsageStackedCountPoint)
	dayMapCredits := make(map[int64]*big.Int)
	dayMapTokens := make(map[int64]*UsageStackedTokenPoint)
	dayComplete := make(map[int64]bool)
	orderedDays := make([]int64, 0)
	for i, start := range starts {
		day := UnixDayStart(start)
		if _, ok := dayMapReq[day]; !ok {
			dayMapReq[day] = &UsageStackedCountPoint{Timestamp: day}
			dayMapCredits[day] = big.NewInt(0)
			dayMapTokens[day] = &UsageStackedTokenPoint{Timestamp: day}
			orderedDays = append(orderedDays, day)
			dayComplete[day] = completeFlags[i]
		}
		row := byPeriod[start]
		dayMapReq[day].SuccessCount += row.SuccessCount
		dayMapReq[day].FailureCount += row.FailureCount
		dayMapCredits[day].Add(dayMapCredits[day], &row.Credits.Int)
		dayMapTokens[day].PromptTokens += row.PromptTokens
		dayMapTokens[day].CompletionTokens += row.CompletionTokens
		if !completeFlags[i] {
			dayComplete[day] = false
		}
	}
	for _, day := range orderedDays {
		req := dayMapReq[day]
		req.Complete = dayComplete[day]
		out.RequestSeries = append(out.RequestSeries, *req)
		out.CreditsSeries = append(out.CreditsSeries, UsageSeriesPoint{
			Timestamp: day,
			Value:     new(big.Int).Set(dayMapCredits[day]),
			Complete:  dayComplete[day],
		})
		tok := dayMapTokens[day]
		tok.Complete = dayComplete[day]
		out.TokenSeries = append(out.TokenSeries, *tok)
	}
	return out, nil
}

func GetProjectModelUsageSnapshots(ctx context.Context, db *gorm.DB, projectID uint, rangeType models.UsageStatsRangeType) ([]models.ProjectModelUsageSnapshot, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var rows []models.ProjectModelUsageSnapshot
	if err := db.WithContext(dbCtx).
		Where("project_id = ? AND range_type = ?", projectID, rangeType).
		Order("`rank` ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func GetProjectDurationHistogramSnapshots(ctx context.Context, db *gorm.DB, projectID uint, rangeType models.UsageStatsRangeType) ([]models.ProjectDurationHistogramSnapshot, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var rows []models.ProjectDurationHistogramSnapshot
	if err := db.WithContext(dbCtx).
		Where("project_id = ? AND range_type = ?", projectID, rangeType).
		Order("bucket_index ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func accountHourlyPeriodStarts(rangeType AccountStatsRange, nowUnix int64) ([]int64, []bool, error) {
	currentHour := HourStartUnix(nowUnix)
	switch rangeType {
	case AccountStatsRange1d:
		starts := make([]int64, 24)
		complete := make([]bool, 24)
		for i := 0; i < 24; i++ {
			starts[i] = currentHour - int64(23-i)*3600
			complete[i] = starts[i] < currentHour
		}
		return starts, complete, nil
	case AccountStatsRange7d:
		return dailyExpandedHourStarts(currentHour, 7)
	case AccountStatsRange1m:
		return dailyExpandedHourStarts(currentHour, 30)
	default:
		return nil, nil, errors.New("invalid account stats range")
	}
}

func projectHourlyPeriodStarts(rangeType ProjectStatsRange, nowUnix int64) ([]int64, []bool, error) {
	currentHour := HourStartUnix(nowUnix)
	switch rangeType {
	case ProjectStatsRange1d:
		starts := make([]int64, 24)
		complete := make([]bool, 24)
		for i := 0; i < 24; i++ {
			starts[i] = currentHour - int64(23-i)*3600
			complete[i] = starts[i] < currentHour
		}
		return starts, complete, nil
	case ProjectStatsRange1m:
		return dailyExpandedHourStarts(currentHour, 30)
	default:
		return nil, nil, errors.New("invalid project stats range")
	}
}

func dailyExpandedHourStarts(currentHour int64, days int) ([]int64, []bool, error) {
	currentDay := UnixDayStart(currentHour)
	var starts []int64
	var complete []bool
	for d := days - 1; d >= 0; d-- {
		dayStart := currentDay - int64(d)*86400
		for h := 0; h < 24; h++ {
			start := dayStart + int64(h)*3600
			if start > currentHour {
				continue
			}
			starts = append(starts, start)
			complete = append(complete, start < currentHour)
		}
	}
	return starts, complete, nil
}
