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
)

var estimateTaskFeeFn = service.EstimateTaskFee

func recordFailedCall(ctx context.Context, project *models.Project, apiType models.TaskAPIType, model string, billedVram uint64, acceptedAt, completedAt time.Time, priorityGwei *big.Int, taskFee *service.CalcTaskFeeResult) error {
	if acceptedAt.IsZero() {
		acceptedAt = completedAt
	}
	if completedAt.IsZero() {
		completedAt = time.Now()
		if acceptedAt.IsZero() {
			acceptedAt = completedAt
		}
	}
	if priorityGwei == nil {
		priorityGwei = &project.PriorityGwei.Int
	}
	in := service.RecordTaskCallInput{
		UserID:               project.UserID,
		ProjectID:            project.ID,
		TaskType:             models.TaskTypeLLM,
		APIType:              apiType,
		Model:                model,
		PriorityGwei:         priorityGwei,
		TokenUsageApplicable: true,
		Status:               models.TaskCallStatusFailed,
		Credits:              big.NewInt(0),
		AcceptedAt:           acceptedAt,
		CompletedAt:          completedAt,
		BilledVram:           billedVram,
		Charge:               false,
	}
	if taskFee != nil {
		in.TaskFeeGwei = taskFee.TaskFeeGwei
		in.MedianPriorityGwei = taskFee.MedianPriorityGwei
		estimated := taskFee.EstimatedNodeSeconds
		weight := taskFee.VramWeight
		in.EstimatedNodeSeconds = &estimated
		in.VramWeight = &weight
	}
	_, err := service.ProcessTaskCall(ctx, config.GetDB(), in)
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
