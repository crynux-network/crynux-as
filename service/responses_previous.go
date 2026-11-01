package service

import (
	"context"
	"crynux_as/models"
	"errors"
	"strings"

	"gorm.io/gorm"
)

// LoadPreviousResponsesJob loads a same-project retained completed Responses job
// for previous_response_id expansion.
func LoadPreviousResponsesJob(
	ctx context.Context,
	db *gorm.DB,
	projectID uint,
	publicID string,
	retentionDays uint64,
) (*models.TaskJob, error) {
	job, err := GetTaskJobByPublicID(ctx, db, projectID, strings.TrimSpace(publicID))
	if err != nil {
		if errors.Is(err, ErrTaskJobNotFound) {
			return nil, ErrPreviousResponsesInvalid
		}
		return nil, err
	}
	if job.APIType != models.TaskAPITypeResponses {
		return nil, ErrPreviousResponsesInvalid
	}
	if job.Status != models.TaskJobStatusCompleted {
		return nil, ErrPreviousResponsesInvalid
	}
	if IsResponsesJobExpired(job, retentionDays) {
		return nil, ErrPreviousResponsesInvalid
	}
	return job, nil
}

// IsResponsesJobExpired reports whether a Responses job is outside retention.
func IsResponsesJobExpired(job *models.TaskJob, retentionDays uint64) bool {
	if job == nil || retentionDays == 0 {
		return true
	}
	if job.CompletedAt == nil {
		return false
	}
	return job.CompletedAt.Before(TaskJobRetentionCutoff(retentionDays))
}

// ErrPreviousResponsesInvalid indicates previous_response_id cannot be used.
var ErrPreviousResponsesInvalid = errors.New("previous response is invalid")
