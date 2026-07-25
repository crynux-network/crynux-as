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

type GetVramRatiosResponse struct {
	response.Response
	Data []VramRatioData `json:"data"`
}

func GetVramRatios(c *gin.Context) (*GetVramRatiosResponse, error) {
	tiers := config.GetConfig().LLM.VramRatios
	data := make([]VramRatioData, 0, len(tiers))
	for _, tier := range tiers {
		data = append(data, VramRatioData{
			MaxVram: tier.MaxVram,
			Ratio:   tier.Ratio,
		})
	}
	return &GetVramRatiosResponse{Data: data}, nil
}
