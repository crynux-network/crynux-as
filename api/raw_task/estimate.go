package raw_task

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"errors"
	"math/big"
)

// EstimateForProject runs create-time fee validation and Credits estimation without
// locking Credits or creating a task job.
func EstimateForProject(ctx context.Context, project *models.Project, request CreateTaskRequest) (*TaskEstimateView, error) {
	prepared, err := prepareTask(ctx, project, request)
	if err != nil {
		return nil, err
	}
	available, err := service.AvailableCreditsForUser(ctx, config.GetDB(), project.UserID)
	if err != nil {
		return nil, err
	}
	estimated, ok := new(big.Int).SetString(prepared.credits, 10)
	if !ok {
		return nil, errors.New("invalid estimated credits")
	}
	return &TaskEstimateView{
		EstimatedCredits:         prepared.credits,
		PriorityGwei:             prepared.priority,
		TaskFeeGwei:              prepared.taskFeeGwei,
		TaskFeeWei:               prepared.taskFeeWei,
		EstimatedNodeSeconds:     prepared.estimatedNodeSeconds,
		BilledVram:               prepared.billedVram,
		VramWeight:               prepared.vramWeight,
		Model:                    prepared.model,
		ModelReady:               rawTaskModelReady(prepared),
		AvailableCredits:         available.String(),
		Sufficient:               available.Cmp(estimated) >= 0,
		PromptTokensEstimate:     prepared.promptTokensEstimate,
		CompletionTokensEstimate: prepared.completionTokensEstimate,
	}, nil
}
