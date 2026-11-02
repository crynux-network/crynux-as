package llmadapter

import "crynux_as/models"

// correctFinishReasonForLengthCap rewrites finish_reason from "length" to "stop"
// when completion_tokens is below max_new_tokens. Temporary workaround until the
// gpt-task multi-EOS finish_reason fix ships with node releases.
func correctFinishReasonForLengthCap(finishReason string, completionTokens, maxNewTokens int) string {
	if finishReason == string(models.FinishReasonLength) && maxNewTokens > 0 && completionTokens < maxNewTokens {
		return string(models.FinishReasonStop)
	}
	return finishReason
}

func maxNewTokensFromTaskArgs(taskArgs *models.GPTTaskArgs) int {
	if taskArgs == nil || taskArgs.GenerationConfig == nil {
		return 0
	}
	return taskArgs.GenerationConfig.MaxNewTokens
}
