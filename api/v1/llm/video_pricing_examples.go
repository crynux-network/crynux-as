package llm

import (
	"crynux_as/api/v1/response"
	"crynux_as/relay"
	"crynux_as/service"

	"github.com/gin-gonic/gin"
)

const (
	videoExampleWidth     uint64 = 832
	videoExampleHeight    uint64 = 480
	videoExampleNumFrames uint64 = 81
	videoExampleSteps     uint64 = 20
	videoExampleNumVideos uint64 = 1
)

type VideoPricingExampleRow struct {
	Model               string         `json:"model" description:"Selected model id"`
	MinVRAM             uint64         `json:"min_vram" description:"Model minimum VRAM in GB"`
	DType               string         `json:"dtype" description:"Selected dtype"`
	QuantizationConfig  any            `json:"quantization_config" description:"Selected quantization_config"`
	Components          map[string]any `json:"components" description:"Executable component replacements for this Video row"`
	OverheadSeconds     float64        `json:"overhead_seconds" description:"Relay video overhead seconds"`
	SecondsPerVideoUnit float64        `json:"seconds_per_video_unit" description:"Relay seconds per video unit (width x height x num_frames x steps)"`
}

type VideoPricingExamplesData struct {
	ExampleWidth     uint64                   `json:"example_width" description:"Video width for Credits and execution-time examples"`
	ExampleHeight    uint64                   `json:"example_height" description:"Video height for Credits and execution-time examples"`
	ExampleNumFrames uint64                   `json:"example_num_frames" description:"Number of frames for Credits and execution-time examples"`
	ExampleSteps     uint64                   `json:"example_steps" description:"Diffusion steps for Credits and execution-time examples"`
	ExampleNumVideos uint64                   `json:"example_num_videos" description:"Number of videos for Credits and execution-time examples"`
	Examples         []VideoPricingExampleRow `json:"examples" description:"Selected per-model video coefficient rows"`
}

type GetVideoPricingExamplesResponse struct {
	response.Response
	Data *VideoPricingExamplesData `json:"data"`
}

func GetVideoPricingExamples(c *gin.Context) (*GetVideoPricingExamplesResponse, error) {
	selected := selectDiffusionPricingExampleModels(service.ListLoadedVideoModels())
	examples := make([]VideoPricingExampleRow, 0, len(selected))
	for _, model := range selected {
		minVRAM := model.MinVRAM
		query := relay.ExecutionTimeQuery{
			Model:              model.ModelID,
			Dtype:              model.DType,
			QuantizationConfig: service.QuantizationConfigForAPI(model.QuantizationConfig),
			MinVRAM:            &minVRAM,
		}
		coefficients, err := service.GetVideoExecutionTime(c.Request.Context(), query)
		if err != nil {
			return nil, err
		}
		examples = append(examples, VideoPricingExampleRow{
			Model:               model.ModelID,
			MinVRAM:             model.MinVRAM,
			DType:               model.DType,
			QuantizationConfig:  service.QuantizationConfigForAPI(model.QuantizationConfig),
			Components:          model.Components,
			OverheadSeconds:     coefficients.OverheadSeconds,
			SecondsPerVideoUnit: coefficients.SecondsPerVideoUnit,
		})
	}

	return &GetVideoPricingExamplesResponse{
		Data: &VideoPricingExamplesData{
			ExampleWidth:     videoExampleWidth,
			ExampleHeight:    videoExampleHeight,
			ExampleNumFrames: videoExampleNumFrames,
			ExampleSteps:     videoExampleSteps,
			ExampleNumVideos: videoExampleNumVideos,
			Examples:         examples,
		},
	}, nil
}
