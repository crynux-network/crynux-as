package llm

import (
	"crynux_as/api/v1/response"
	"crynux_as/config"
	"crynux_as/service"

	"github.com/gin-gonic/gin"
)

type BillingConfigData struct {
	BaseVRAM              uint64  `json:"base_vram" description:"Base VRAM in GB used for vram_weight"`
	ReferencePriorityGwei string  `json:"reference_priority_gwei" description:"Fixed reference priority in Gwei"`
	CreditsPerGwei        uint64  `json:"credits_per_gwei" description:"Credits charged per billable Gwei"`
	MaxTokenRatio         uint64  `json:"max_token_ratio" description:"Maximum allowed cost level token_ratio display value"`
	MedianPriorityGwei    string  `json:"median_priority_gwei" description:"Current queue median priority hint in Gwei"`
	HighestPriorityGwei   *string `json:"highest_priority_gwei" description:"Current queue highest priority in Gwei when the queue is non-empty"`
	LowestPriorityGwei    *string `json:"lowest_priority_gwei" description:"Current queue lowest priority in Gwei when the queue is non-empty"`
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
	data := &BillingConfigData{
		BaseVRAM:              llmCfg.BaseVRAM,
		ReferencePriorityGwei: llmCfg.ReferencePriorityGwei,
		CreditsPerGwei:        llmCfg.CreditsPerGwei,
		MaxTokenRatio:         llmCfg.MaxTokenRatio,
		MedianPriorityGwei:    median.String(),
	}
	if highest, lowest, ok := service.ResolveQueuePriorityBounds(); ok {
		highestStr := highest.String()
		lowestStr := lowest.String()
		data.HighestPriorityGwei = &highestStr
		data.LowestPriorityGwei = &lowestStr
	}
	return &GetBillingConfigResponse{Data: data}, nil
}
