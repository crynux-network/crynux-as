package llm

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"math/big"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var estimateTaskFeeFn = service.EstimateTaskFee

func loadCreditAccount(ctx context.Context, db *gorm.DB, userID uint) (*models.CreditAccount, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var account models.CreditAccount
	if err := db.WithContext(dbCtx).Where("user_id = ?", userID).First(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func recordFailedCall(ctx context.Context, project *models.Project, model string, billedVram uint64, duration time.Duration, taskFee *service.CalcTaskFeeResult) error {
	in := service.RecordLLMCallInput{
		UserID:     project.UserID,
		ProjectID:  project.ID,
		Model:      model,
		TokenRatio: project.TokenRatio,
		Status:     models.LLMCallStatusFailed,
		Credits:    big.NewInt(0),
		DurationMs: uint64(duration.Milliseconds()),
		BilledVram: billedVram,
		Charge:     false,
	}
	if taskFee != nil {
		in.TaskFeeGwei = taskFee.TaskFeeGwei
		in.MedianPriorityGwei = taskFee.MedianPriorityGwei
		estimated := taskFee.EstimatedNodeSeconds
		weight := taskFee.VramWeight
		in.EstimatedNodeSeconds = &estimated
		in.VramWeight = &weight
	}
	_, err := service.ProcessLLMCall(ctx, config.GetDB(), in)
	return err
}

func writeClientError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}

func writeServerError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": gin.H{
			"message": "internal server error",
			"type":    "server_error",
		},
	})
}
