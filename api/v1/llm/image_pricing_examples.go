package llm

import (
	"crynux_as/api/v1/response"
	"crynux_as/relay"
	"crynux_as/service"
	"math"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	imageExampleWidth     uint64 = 512
	imageExampleHeight    uint64 = 512
	imageExampleSteps     uint64 = 20
	imageExampleNumImages uint64 = 1
)

type ImagePricingExampleRow struct {
	Model                 string         `json:"model" description:"Selected model id"`
	MinVRAM               uint64         `json:"min_vram" description:"Model minimum VRAM in GB"`
	ExecutionDType        string         `json:"execution_dtype" description:"Selected execution dtype"`
	QuantizationConfig    any            `json:"quantization_config" description:"Selected quantization_config"`
	Components            map[string]any `json:"components" description:"Executable component replacements for this Image row"`
	OverheadSeconds       float64        `json:"overhead_seconds" description:"Relay SD overhead seconds"`
	SecondsPerSDPixelStep float64        `json:"seconds_per_sd_pixel_step" description:"Relay seconds per SD pixel-step unit"`
}

type ImagePricingExamplesData struct {
	ExampleWidth     uint64                   `json:"example_width" description:"Image width for Credits and execution-time examples"`
	ExampleHeight    uint64                   `json:"example_height" description:"Image height for Credits and execution-time examples"`
	ExampleSteps     uint64                   `json:"example_steps" description:"Diffusion steps for Credits and execution-time examples"`
	ExampleNumImages uint64                   `json:"example_num_images" description:"Number of images for Credits and execution-time examples"`
	Examples         []ImagePricingExampleRow `json:"examples" description:"Selected per-model SD coefficient rows"`
}

type GetImagePricingExamplesResponse struct {
	response.Response
	Data *ImagePricingExamplesData `json:"data"`
}

func GetImagePricingExamples(c *gin.Context) (*GetImagePricingExamplesResponse, error) {
	selected := selectImagePricingExampleModels(service.ListLoadedSDModels())
	examples := make([]ImagePricingExampleRow, 0, len(selected))
	for _, model := range selected {
		minVRAM := model.MinVRAM
		query := relay.ExecutionTimeQuery{
			Model:              model.ModelID,
			Dtype:              model.ExecutionDType,
			QuantizationConfig: service.QuantizationConfigForAPI(model.QuantizationConfig),
			MinVRAM:            &minVRAM,
		}
		coefficients, err := service.GetSDExecutionTime(c.Request.Context(), query)
		if err != nil {
			return nil, err
		}
		examples = append(examples, ImagePricingExampleRow{
			Model:                 model.ModelID,
			MinVRAM:               model.MinVRAM,
			ExecutionDType:        model.ExecutionDType,
			QuantizationConfig:    service.QuantizationConfigForAPI(model.QuantizationConfig),
			Components:            model.Components,
			OverheadSeconds:       coefficients.OverheadSeconds,
			SecondsPerSDPixelStep: coefficients.SecondsPerSDPixelStep,
		})
	}

	return &GetImagePricingExamplesResponse{
		Data: &ImagePricingExamplesData{
			ExampleWidth:     imageExampleWidth,
			ExampleHeight:    imageExampleHeight,
			ExampleSteps:     imageExampleSteps,
			ExampleNumImages: imageExampleNumImages,
			Examples:         examples,
		},
	}, nil
}

func imagePricingExampleGroupKey(model service.LoadedLLMModel) string {
	return model.ModelID + "\x00" +
		model.QuantizationKey + "\x00" +
		model.ComponentsKey + "\x00" +
		strconv.FormatUint(model.MinVRAM, 10)
}

func selectImagePricingExampleModels(models []service.LoadedLLMModel) []service.LoadedLLMModel {
	// Keep one row per (model_id, quantization_config, components, min_vram).
	// Same identity with different execution_dtype collapses to the strongest
	// node-count row.
	byIdentity := map[string]service.LoadedLLMModel{}
	for _, model := range models {
		if model.MinVRAM == 0 {
			continue
		}
		key := imagePricingExampleGroupKey(model)
		existing, ok := byIdentity[key]
		if !ok || betterPricingExampleRow(model, existing) {
			byIdentity[key] = model
		}
	}

	representatives := make([]service.LoadedLLMModel, 0, len(byIdentity))
	for _, model := range byIdentity {
		representatives = append(representatives, model)
	}
	sort.Slice(representatives, func(i, j int) bool {
		if representatives[i].MinVRAM != representatives[j].MinVRAM {
			return representatives[i].MinVRAM < representatives[j].MinVRAM
		}
		if representatives[i].ModelID != representatives[j].ModelID {
			return representatives[i].ModelID < representatives[j].ModelID
		}
		if representatives[i].QuantizationKey != representatives[j].QuantizationKey {
			return representatives[i].QuantizationKey < representatives[j].QuantizationKey
		}
		return representatives[i].ComponentsKey < representatives[j].ComponentsKey
	})

	n := len(representatives)
	if n == 0 {
		return nil
	}
	if n <= 4 {
		return representatives
	}

	indexes := []int{
		0,
		int(math.Floor(float64(n-1) / 3)),
		int(math.Floor(2 * float64(n-1) / 3)),
		n - 1,
	}
	selected := make([]service.LoadedLLMModel, 0, 4)
	seen := map[int]struct{}{}
	for _, idx := range indexes {
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		selected = append(selected, representatives[idx])
	}
	return selected
}
