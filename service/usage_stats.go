package service

import (
	"context"
	"crynux_as/models"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const usageStatsBatchSize = 200

// RunUsageStatsBaseAggregation processes unaggregated terminal call records into base stats tables.
// It returns the number of call records processed in this batch.
func RunUsageStatsBaseAggregation(ctx context.Context, db *gorm.DB) (int, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var processed int
	err := db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var progress models.UsageStatsProgress
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("project_id = ?", models.UsageStatsBaseProjectID).
			First(&progress).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var maxID uint
			if err := tx.Model(&models.LLMCallRecord{}).
				Select("COALESCE(MAX(id), 0)").
				Scan(&maxID).Error; err != nil {
				return err
			}
			progress = models.UsageStatsProgress{
				ProjectID:        models.UsageStatsBaseProjectID,
				LastCallRecordID: maxID,
			}
			if err := tx.Create(&progress).Error; err != nil {
				return err
			}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("project_id = ?", models.UsageStatsBaseProjectID).
				First(&progress).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		var records []models.LLMCallRecord
		if err := tx.Where("id > ?", progress.LastCallRecordID).
			Order("id ASC").
			Limit(usageStatsBatchSize).
			Find(&records).Error; err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}

		creditAmounts := make(map[uint]*big.Int, len(records))
		recordIDs := make([]uint, 0, len(records))
		for _, record := range records {
			recordIDs = append(recordIDs, record.ID)
			creditAmounts[record.ID] = big.NewInt(0)
		}

		var events []models.CreditEvent
		if err := tx.Where(
			"type = ? AND status = ? AND ref_id IN ?",
			models.CreditEventTypeLLMCharge,
			models.CreditEventStatusProcessed,
			recordIDs,
		).Find(&events).Error; err != nil {
			return err
		}
		for _, event := range events {
			creditAmounts[event.RefID] = new(big.Int).Set(&event.Amount.Int)
		}

		dirtyProjects := make(map[uint]struct{})
		for _, record := range records {
			credits := creditAmounts[record.ID]
			if credits == nil {
				credits = big.NewInt(0)
			}
			if err := applyCallRecordToBaseStats(tx, record, credits); err != nil {
				return err
			}
			dirtyProjects[record.ProjectID] = struct{}{}
			progress.LastCallRecordID = record.ID
		}

		for projectID := range dirtyProjects {
			if err := markProjectSnapshotDirty(tx, projectID); err != nil {
				return err
			}
		}

		if err := tx.Model(&models.UsageStatsProgress{}).
			Where("project_id = ?", models.UsageStatsBaseProjectID).
			Updates(map[string]interface{}{
				"last_call_record_id": progress.LastCallRecordID,
			}).Error; err != nil {
			return err
		}
		processed = len(records)
		return nil
	})
	return processed, err
}

func applyCallRecordToBaseStats(tx *gorm.DB, record models.LLMCallRecord, credits *big.Int) error {
	if record.UserID == 0 || record.ProjectID == 0 {
		return fmt.Errorf("call record %d missing user_id or project_id", record.ID)
	}
	hourStart := HourStartUnix(record.AcceptedAt.Unix())
	tenMinStart := TenMinuteStartUnix(record.AcceptedAt.Unix())

	var successInc, failureInc uint64
	if record.Status == models.LLMCallStatusSuccess {
		successInc = 1
	} else {
		failureInc = 1
	}

	var promptInc, completionInc, totalInc uint64
	if record.TokenUsageApplicable != 0 {
		promptInc = record.PromptTokens
		completionInc = record.CompletionTokens
		totalInc = record.TotalTokens
	}

	if err := upsertAccountHourlyStat(tx, record.UserID, hourStart, successInc, failureInc, promptInc, completionInc, totalInc, credits); err != nil {
		return err
	}
	if err := upsertProjectHourlyStat(tx, record.ProjectID, record.UserID, hourStart, successInc, failureInc, promptInc, completionInc, totalInc, credits); err != nil {
		return err
	}
	if err := upsertProjectModel10mStat(tx, record.ProjectID, record.UserID, record.Model, tenMinStart, successInc, failureInc, promptInc, completionInc, totalInc, credits); err != nil {
		return err
	}
	bucketID := DurationBucketID(record.DurationMs)
	return upsertProjectDuration10mStat(tx, record.ProjectID, record.UserID, tenMinStart, bucketID, 1)
}

func upsertAccountHourlyStat(
	tx *gorm.DB,
	userID uint,
	periodStart int64,
	successInc, failureInc, promptInc, completionInc, totalInc uint64,
	credits *big.Int,
) error {
	var row models.AccountUsageHourlyStat
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND period_start = ?", userID, periodStart).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = models.AccountUsageHourlyStat{
			UserID:           userID,
			PeriodStart:      periodStart,
			RequestCount:     successInc + failureInc,
			SuccessCount:     successInc,
			FailureCount:     failureInc,
			PromptTokens:     promptInc,
			CompletionTokens: completionInc,
			TotalTokens:      totalInc,
			Credits:          models.BigInt{Int: *new(big.Int).Set(credits)},
		}
		return tx.Create(&row).Error
	}
	if err != nil {
		return err
	}
	row.RequestCount += successInc + failureInc
	row.SuccessCount += successInc
	row.FailureCount += failureInc
	row.PromptTokens += promptInc
	row.CompletionTokens += completionInc
	row.TotalTokens += totalInc
	sum := new(big.Int).Add(&row.Credits.Int, credits)
	row.Credits = models.BigInt{Int: *sum}
	return tx.Save(&row).Error
}

func upsertProjectHourlyStat(
	tx *gorm.DB,
	projectID, userID uint,
	periodStart int64,
	successInc, failureInc, promptInc, completionInc, totalInc uint64,
	credits *big.Int,
) error {
	var row models.ProjectUsageHourlyStat
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("project_id = ? AND period_start = ?", projectID, periodStart).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = models.ProjectUsageHourlyStat{
			ProjectID:        projectID,
			UserID:           userID,
			PeriodStart:      periodStart,
			RequestCount:     successInc + failureInc,
			SuccessCount:     successInc,
			FailureCount:     failureInc,
			PromptTokens:     promptInc,
			CompletionTokens: completionInc,
			TotalTokens:      totalInc,
			Credits:          models.BigInt{Int: *new(big.Int).Set(credits)},
		}
		return tx.Create(&row).Error
	}
	if err != nil {
		return err
	}
	row.RequestCount += successInc + failureInc
	row.SuccessCount += successInc
	row.FailureCount += failureInc
	row.PromptTokens += promptInc
	row.CompletionTokens += completionInc
	row.TotalTokens += totalInc
	sum := new(big.Int).Add(&row.Credits.Int, credits)
	row.Credits = models.BigInt{Int: *sum}
	return tx.Save(&row).Error
}

func upsertProjectModel10mStat(
	tx *gorm.DB,
	projectID, userID uint,
	model string,
	periodStart int64,
	successInc, failureInc, promptInc, completionInc, totalInc uint64,
	credits *big.Int,
) error {
	var row models.ProjectModelUsage10mStat
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("project_id = ? AND model = ? AND period_start = ?", projectID, model, periodStart).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = models.ProjectModelUsage10mStat{
			ProjectID:        projectID,
			UserID:           userID,
			Model:            model,
			PeriodStart:      periodStart,
			RequestCount:     successInc + failureInc,
			SuccessCount:     successInc,
			FailureCount:     failureInc,
			PromptTokens:     promptInc,
			CompletionTokens: completionInc,
			TotalTokens:      totalInc,
			Credits:          models.BigInt{Int: *new(big.Int).Set(credits)},
		}
		return tx.Create(&row).Error
	}
	if err != nil {
		return err
	}
	row.RequestCount += successInc + failureInc
	row.SuccessCount += successInc
	row.FailureCount += failureInc
	row.PromptTokens += promptInc
	row.CompletionTokens += completionInc
	row.TotalTokens += totalInc
	sum := new(big.Int).Add(&row.Credits.Int, credits)
	row.Credits = models.BigInt{Int: *sum}
	return tx.Save(&row).Error
}

func upsertProjectDuration10mStat(tx *gorm.DB, projectID, userID uint, periodStart int64, bucketID string, requestInc uint64) error {
	var row models.ProjectDurationUsage10mStat
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("project_id = ? AND period_start = ? AND duration_bucket = ?", projectID, periodStart, bucketID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = models.ProjectDurationUsage10mStat{
			ProjectID:      projectID,
			UserID:         userID,
			PeriodStart:    periodStart,
			DurationBucket: bucketID,
			RequestCount:   requestInc,
		}
		return tx.Create(&row).Error
	}
	if err != nil {
		return err
	}
	row.RequestCount += requestInc
	return tx.Save(&row).Error
}

func markProjectSnapshotDirty(tx *gorm.DB, projectID uint) error {
	var progress models.UsageStatsProgress
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("project_id = ?", projectID).
		First(&progress).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		progress = models.UsageStatsProgress{
			ProjectID:       projectID,
			DirtyGeneration: 1,
		}
		return tx.Create(&progress).Error
	}
	if err != nil {
		return err
	}
	progress.DirtyGeneration++
	return tx.Save(&progress).Error
}

// ClaimDirtyProjectsForSnapshot claims up to limit projects that need snapshot refresh.
func ClaimDirtyProjectsForSnapshot(ctx context.Context, db *gorm.DB, limit int) ([]models.UsageStatsProgress, error) {
	if limit <= 0 {
		limit = 20
	}
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var claimed []models.UsageStatsProgress
	err := db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var rows []models.UsageStatsProgress
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("project_id <> ? AND dirty_generation > claimed_generation", models.UsageStatsBaseProjectID).
			Order("project_id ASC").
			Limit(limit).
			Find(&rows).Error; err != nil {
			return err
		}
		now := time.Now()
		for i := range rows {
			rows[i].ClaimedGeneration = rows[i].DirtyGeneration
			rows[i].ClaimedAt = &now
			if err := tx.Save(&rows[i]).Error; err != nil {
				return err
			}
			claimed = append(claimed, rows[i])
		}
		return nil
	})
	return claimed, err
}

// RefreshProjectUsageSnapshots rebuilds model Top-10 and duration histogram snapshots for one project.
func RefreshProjectUsageSnapshots(ctx context.Context, db *gorm.DB, projectID uint, claimedGeneration uint64) error {
	now := time.Now().Unix()
	ranges := []struct {
		rangeType models.UsageStatsRangeType
		window    func(now int64) (int64, int64)
	}{
		{models.UsageStatsRange1h, windowLast1Hour10m},
		{models.UsageStatsRange1d, windowLast1Day10m},
		{models.UsageStatsRange7d, windowLast7Days10m},
	}

	dbCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		for _, item := range ranges {
			windowStart, windowEnd := item.window(now)
			if err := rebuildModelSnapshot(tx, projectID, item.rangeType, windowStart, windowEnd); err != nil {
				return err
			}
			if err := rebuildDurationSnapshot(tx, projectID, item.rangeType, windowStart, windowEnd); err != nil {
				return err
			}
		}

		var progress models.UsageStatsProgress
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("project_id = ?", projectID).
			First(&progress).Error; err != nil {
			return err
		}
		if progress.DirtyGeneration != claimedGeneration {
			log.Infof(
				"usage snapshot for project %d left dirty: dirty=%d claimed=%d",
				projectID, progress.DirtyGeneration, claimedGeneration,
			)
			return nil
		}
		progress.ClaimedGeneration = progress.DirtyGeneration
		return tx.Save(&progress).Error
	})
}

func windowLast1Hour10m(now int64) (int64, int64) {
	current := TenMinuteStartUnix(now)
	return current - 5*600, current + 600
}

func windowLast1Day10m(now int64) (int64, int64) {
	current := TenMinuteStartUnix(now)
	return current - 143*600, current + 600
}

func windowLast7Days10m(now int64) (int64, int64) {
	current := TenMinuteStartUnix(now)
	return current - 1007*600, current + 600
}

func rebuildModelSnapshot(tx *gorm.DB, projectID uint, rangeType models.UsageStatsRangeType, windowStart, windowEnd int64) error {
	if err := tx.Where("project_id = ? AND range_type = ?", projectID, rangeType).
		Delete(&models.ProjectModelUsageSnapshot{}).Error; err != nil {
		return err
	}

	var rows []models.ProjectModelUsage10mStat
	if err := tx.Where("project_id = ? AND period_start >= ? AND period_start < ?", projectID, windowStart, windowEnd).
		Find(&rows).Error; err != nil {
		return err
	}

	type agg struct {
		Model            string
		RequestCount     uint64
		SuccessCount     uint64
		FailureCount     uint64
		PromptTokens     uint64
		CompletionTokens uint64
		TotalTokens      uint64
		Credits          *big.Int
	}
	byModel := make(map[string]*agg)
	for _, row := range rows {
		item, ok := byModel[row.Model]
		if !ok {
			item = &agg{Model: row.Model, Credits: big.NewInt(0)}
			byModel[row.Model] = item
		}
		item.RequestCount += row.RequestCount
		item.SuccessCount += row.SuccessCount
		item.FailureCount += row.FailureCount
		item.PromptTokens += row.PromptTokens
		item.CompletionTokens += row.CompletionTokens
		item.TotalTokens += row.TotalTokens
		item.Credits.Add(item.Credits, &row.Credits.Int)
	}

	list := make([]*agg, 0, len(byModel))
	for _, item := range byModel {
		list = append(list, item)
	}
	sort.Slice(list, func(i, j int) bool {
		cmp := list[i].Credits.Cmp(list[j].Credits)
		if cmp != 0 {
			return cmp > 0
		}
		if list[i].RequestCount != list[j].RequestCount {
			return list[i].RequestCount > list[j].RequestCount
		}
		return list[i].Model < list[j].Model
	})
	if len(list) > 10 {
		list = list[:10]
	}

	for i, item := range list {
		snap := models.ProjectModelUsageSnapshot{
			ProjectID:        projectID,
			RangeType:        rangeType,
			Rank:             uint(i + 1),
			Model:            item.Model,
			RequestCount:     item.RequestCount,
			SuccessCount:     item.SuccessCount,
			FailureCount:     item.FailureCount,
			PromptTokens:     item.PromptTokens,
			CompletionTokens: item.CompletionTokens,
			TotalTokens:      item.TotalTokens,
			Credits:          models.BigInt{Int: *new(big.Int).Set(item.Credits)},
			WindowStart:      windowStart,
			WindowEnd:        windowEnd,
		}
		if err := tx.Create(&snap).Error; err != nil {
			return err
		}
	}
	return nil
}

func rebuildDurationSnapshot(tx *gorm.DB, projectID uint, rangeType models.UsageStatsRangeType, windowStart, windowEnd int64) error {
	if err := tx.Where("project_id = ? AND range_type = ?", projectID, rangeType).
		Delete(&models.ProjectDurationHistogramSnapshot{}).Error; err != nil {
		return err
	}

	var rows []models.ProjectDurationUsage10mStat
	if err := tx.Where("project_id = ? AND period_start >= ? AND period_start < ?", projectID, windowStart, windowEnd).
		Find(&rows).Error; err != nil {
		return err
	}

	counts := make(map[string]uint64)
	for _, row := range rows {
		counts[row.DurationBucket] += row.RequestCount
	}
	display := MergeDurationDisplayBuckets(counts)
	for _, bucket := range display {
		snap := models.ProjectDurationHistogramSnapshot{
			ProjectID:     projectID,
			RangeType:     rangeType,
			BucketIndex:   bucket.BucketIndex,
			BucketLabel:   bucket.BucketLabel,
			MinDurationMs: bucket.MinDurationMs,
			MaxDurationMs: bucket.MaxDurationMs,
			RequestCount:  bucket.RequestCount,
			WindowStart:   windowStart,
			WindowEnd:     windowEnd,
		}
		if err := tx.Create(&snap).Error; err != nil {
			return err
		}
	}
	return nil
}
