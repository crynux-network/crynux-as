package llm

import (
	"crynux_as/api/v1/response"
	"crynux_as/config"

	"github.com/gin-gonic/gin"
)

type VramRatioData struct {
	MaxVram uint64  `json:"max_vram" description:"Inclusive upper bound of the tier in GB"`
	Ratio   float64 `json:"ratio" description:"Billing ratio applied to the tier"`
}

type BillingConfigData struct {
	PromptCreditsPerToken     uint64          `json:"prompt_credits_per_token" description:"Credits charged per billed prompt token before ratio scaling"`
	CompletionCreditsPerToken uint64          `json:"completion_credits_per_token" description:"Credits charged per billed completion token before ratio scaling"`
	VramRatios                []VramRatioData `json:"vram_ratios" description:"Configured VRAM billing ratio tiers"`
}

type GetBillingConfigResponse struct {
	response.Response
	Data *BillingConfigData `json:"data"`
}

func GetBillingConfig(c *gin.Context) (*GetBillingConfigResponse, error) {
	llmCfg := config.GetConfig().LLM
	tiers := make([]VramRatioData, 0, len(llmCfg.VramRatios))
	for _, tier := range llmCfg.VramRatios {
		tiers = append(tiers, VramRatioData{
			MaxVram: tier.MaxVram,
			Ratio:   tier.Ratio,
		})
	}
	return &GetBillingConfigResponse{
		Data: &BillingConfigData{
			PromptCreditsPerToken:     llmCfg.PromptCreditsPerToken,
			CompletionCreditsPerToken: llmCfg.CompletionCreditsPerToken,
			VramRatios:                tiers,
		},
	}, nil
}
