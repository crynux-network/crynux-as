package llm

import (
	"fmt"

	"crynux_as/api/v1/response"
	"crynux_as/config"
	"crynux_as/service"

	"github.com/gin-gonic/gin"
)

type BillingConfigData struct {
	BaseVRAM                     uint64  `json:"base_vram" description:"Base VRAM in GB used for vram_weight"`
	CreditsPerGwei               string  `json:"credits_per_gwei" description:"Credits charged per billable Gwei as a positive decimal string"`
	MinPriorityGwei              string  `json:"min_priority_gwei" description:"Minimum allowed project Cost Level priority_gwei"`
	MaxPriorityGwei              string  `json:"max_priority_gwei" description:"Maximum allowed project Cost Level priority_gwei"`
	MedianPriorityGwei           string  `json:"median_priority_gwei" description:"Current Crynux queue median priority hint in Gwei"`
	HighestPriorityGwei          *string `json:"highest_priority_gwei" description:"Current Crynux queue highest priority in Gwei when the queue is non-empty"`
	LowestPriorityGwei           *string `json:"lowest_priority_gwei" description:"Current Crynux queue lowest priority in Gwei when the queue is non-empty"`
	DefaultAutoMaxCreditsPerTask string  `json:"default_auto_max_credits_per_task" description:"Default Auto Max Credits per task for new projects"`
	ProviderMinPriorityGwei      string  `json:"provider_min_priority_gwei" description:"Shared Provider Cost Level lower bound for the reference 10-input / 50-output workload"`
	ProviderMinCredits           string  `json:"provider_min_credits" description:"Credits covering every configured Provider model at provider_min_priority_gwei for the reference 10-input / 50-output workload"`
	ProviderMinReferenceInput    string  `json:"provider_min_reference_input_tokens" description:"Input token count used to derive Provider minimum Cost Level and Credits hints"`
	ProviderMinReferenceOutput   string  `json:"provider_min_reference_output_tokens" description:"Maximum output token count used to derive Provider minimum Cost Level and Credits hints"`
	ProviderHighestPriorityGwei  *string `json:"provider_highest_priority_gwei" description:"Current Provider queue highest priority in Gwei when the queue is non-empty"`
	ProviderLowestPriorityGwei   *string `json:"provider_lowest_priority_gwei" description:"Current Provider queue lowest priority in Gwei when the queue is non-empty"`
	ProviderQueueHasJobs         bool    `json:"provider_queue_has_jobs" description:"True when the Provider queue snapshot contains non-expired queued jobs"`
}

type GetBillingConfigResponse struct {
	response.Response
	Data *BillingConfigData `json:"data"`
}

func GetBillingConfig(c *gin.Context) (*GetBillingConfigResponse, error) {
	llmCfg := config.GetConfig().LLM
	median, err := service.ResolveQueueMedianHint()
	if err != nil {
		return nil, err
	}
	hints, err := service.ResolveProviderCostHints()
	if err != nil {
		return nil, err
	}
	defaultAutoMax, err := config.GetConfig().ParseDefaultAutoMaxCreditsPerTask()
	if err != nil {
		return nil, err
	}
	data := &BillingConfigData{
		BaseVRAM:                     llmCfg.BaseVRAM,
		CreditsPerGwei:               llmCfg.CreditsPerGwei,
		MinPriorityGwei:              llmCfg.MinPriorityGwei,
		MaxPriorityGwei:              llmCfg.MaxPriorityGwei,
		MedianPriorityGwei:           median.String(),
		DefaultAutoMaxCreditsPerTask: defaultAutoMax.String(),
		ProviderMinPriorityGwei:      hints.MinPriorityGwei.String(),
		ProviderMinCredits:           hints.MinCredits.String(),
		ProviderMinReferenceInput:    fmt.Sprintf("%d", hints.ReferenceInputTokens),
		ProviderMinReferenceOutput:   fmt.Sprintf("%d", hints.ReferenceOutputTokens),
	}
	if highest, lowest, ok := service.ResolveQueuePriorityBounds(); ok {
		highestStr := highest.String()
		lowestStr := lowest.String()
		data.HighestPriorityGwei = &highestStr
		data.LowestPriorityGwei = &lowestStr
	}
	if lowest, highest, hasJobs := service.ProviderQueueBoundsForUI(); hasJobs {
		highestStr := highest.String()
		lowestStr := lowest.String()
		data.ProviderHighestPriorityGwei = &highestStr
		data.ProviderLowestPriorityGwei = &lowestStr
		data.ProviderQueueHasJobs = true
	}
	return &GetBillingConfigResponse{Data: data}, nil
}
