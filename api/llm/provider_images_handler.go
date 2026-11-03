package llm

import (
	"crynux_as/config"
	"crynux_as/llmadapter"
	"crynux_as/models"
	"crynux_as/provider"
	"crynux_as/service"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

func extractImagesModel(body []byte) string {
	var raw struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &raw)
	return strings.ToLower(strings.TrimSpace(raw.Model))
}

func handleProviderImagesRequest(c *gin.Context, project *models.Project, apiType models.TaskAPIType, body []byte, route service.ProviderModelRoute) {
	if route.APIKind != "" && route.APIKind != config.ProviderAPIKindImage {
		writeClientError(c, http.StatusBadRequest, "model does not support the Images API")
		return
	}
	if c.Param("vram_limit") != "" {
		writeClientError(c, http.StatusBadRequest, "vram_limit is not supported for Provider models")
		return
	}

	canonical, err := provider.ParseCanonicalImagesJSON(body, apiType)
	if err != nil {
		writeClientError(c, http.StatusBadRequest, err.Error())
		return
	}
	canonical.Model = route.ModelName

	switch route.ServiceProvider {
	case models.ServiceProviderOpenAI:
		if _, err := provider.ValidateAndBuildOpenAIImagesRequest(canonical); err != nil {
			writeClientError(c, http.StatusBadRequest, err.Error())
			return
		}
	case models.ServiceProviderGoogle:
		if _, err := provider.ValidateAndBuildGoogleImagesRequest(canonical); err != nil {
			writeClientError(c, http.StatusBadRequest, err.Error())
			return
		}
	default:
		writeClientError(c, http.StatusBadRequest, "Service Provider does not support Images")
		return
	}

	// Persist the validated OpenAI-shaped body for admission and adapter execute.
	persistBody, err := rebuildProviderImagesBody(canonical)
	if err != nil {
		writeServerError(c)
		return
	}

	appCfg := config.GetConfig()
	inputEstimate, err := service.EstimateProviderInputTokens(route, apiType, persistBody, appCfg.Provider.MaxInputImageBytes)
	if err != nil {
		writeClientError(c, http.StatusBadRequest, err.Error())
		return
	}
	outputTokens := provider.EstimateProviderImageOutputTokens(
		string(route.ServiceProvider),
		route.ModelName,
		canonical.Quality,
		canonical.Width,
		canonical.Height,
		canonical.N,
	)
	priorityGwei, err := service.ResolveProviderEffectivePriority(project)
	if err != nil {
		log.Errorf("resolve provider priority failed: %v", err)
		writeServerError(c)
		return
	}
	snapshot, credits, err := service.BuildProviderBillingData(project, route, priorityGwei, inputEstimate, outputTokens, nil)
	if err != nil {
		if errors.Is(err, service.ErrTaskFeeBelowMinimum) ||
			errors.Is(err, service.ErrAutoMaxCreditsInsufficient) ||
			errors.Is(err, service.ErrMaxCreditsExceeded) {
			writeClientError(c, http.StatusBadRequest, err.Error())
			return
		}
		log.Errorf("provider image billing estimate failed: %v", err)
		writeServerError(c)
		return
	}
	adjustedPriority, ok := new(big.Int).SetString(snapshot.EffectivePriorityGwei, 10)
	if !ok || adjustedPriority.Sign() <= 0 {
		writeServerError(c)
		return
	}
	queueTimeout := service.ResolveProviderQueueTimeout(canonical.QueueTimeout)
	db := config.GetDB()
	job, err := service.ReserveCreditsAndCreateProviderTaskJob(c.Request.Context(), db, service.CreateProviderTaskJobInput{
		Project:         project,
		APIType:         apiType,
		Route:           route,
		RequestBody:     persistBody,
		PriorityGwei:    adjustedPriority,
		CreditsToLock:   credits,
		QueueTimeout:    queueTimeout,
		BillingSnapshot: snapshot,
	})
	if err != nil {
		if errors.Is(err, service.ErrInsufficientBalance) {
			writePaymentRequired(c)
			return
		}
		log.Errorf("create provider image job failed: %v", err)
		writeServerError(c)
		return
	}

	finalJob, err := service.WaitForTaskJob(c.Request.Context(), db, job.ID, service.ResolveTaskJobWaitTimeout(c.Request.Header))
	if err != nil {
		log.Errorf("wait provider image job %d failed: %v", job.ID, err)
		writeServerError(c)
		return
	}
	if finalJob.Status != models.TaskJobStatusCompleted {
		msg := "image generation failed"
		if finalJob.ErrorMessage != nil && *finalJob.ErrorMessage != "" {
			msg = *finalJob.ErrorMessage
		}
		writeClientError(c, http.StatusInternalServerError, msg)
		return
	}
	n := uint64(canonical.N)
	pngs, err := service.ReadImageResultFiles(finalJob.ID, n)
	if err != nil {
		log.Errorf("read provider image results for job %d failed: %v", finalJob.ID, err)
		writeServerError(c)
		return
	}
	created := time.Now().Unix()
	if finalJob.CompletedAt != nil {
		created = finalJob.CompletedAt.Unix()
	}
	outputFormat := canonical.OutputFormat
	if outputFormat == "" {
		outputFormat = "png"
	}
	var meta struct {
		OutputFormat string `json:"output_format"`
		Size         string `json:"size"`
		Quality      string `json:"quality"`
	}
	if finalJob.FormattedResultJSON != nil {
		_ = json.Unmarshal([]byte(*finalJob.FormattedResultJSON), &meta)
	}
	if meta.OutputFormat != "" {
		outputFormat = meta.OutputFormat
	}
	echoSize := canonical.EchoSize
	if meta.Size != "" {
		echoSize = meta.Size
	}
	echoQuality := canonical.Quality
	if meta.Quality != "" {
		echoQuality = meta.Quality
	}
	responseBytes, err := formatProviderImagesResponse(created, echoSize, echoQuality, outputFormat, pngs)
	if err != nil {
		writeServerError(c)
		return
	}
	c.Data(http.StatusOK, "application/json", responseBytes)
}

func rebuildProviderImagesBody(req provider.CanonicalImagesRequest) ([]byte, error) {
	body := map[string]any{
		"model":  req.Model,
		"prompt": req.Prompt,
		"n":      req.N,
		"size":   req.EchoSize,
	}
	if req.Quality != "" {
		body["quality"] = req.Quality
	}
	if req.OutputFormat != "" {
		body["output_format"] = req.OutputFormat
	}
	if req.OutputCompression != nil {
		body["output_compression"] = *req.OutputCompression
	}
	if req.Background != "" {
		body["background"] = req.Background
	}
	if req.Moderation != "" {
		body["moderation"] = req.Moderation
	}
	if req.InputFidelity != "" {
		body["input_fidelity"] = req.InputFidelity
	}
	if req.QueueTimeout != nil {
		body["queue_timeout"] = *req.QueueTimeout
	}
	if req.APIType == models.TaskAPITypeImagesEdits {
		images := make([]map[string]string, 0, len(req.Images))
		for _, img := range req.Images {
			images = append(images, map[string]string{"image_url": img.DataURL})
		}
		body["images"] = images
		if req.Mask != nil {
			body["mask"] = map[string]string{"image_url": req.Mask.DataURL}
		}
		if req.Markup != nil {
			body["markup"] = map[string]string{"image_url": req.Markup.DataURL}
		}
	}
	return json.Marshal(body)
}

func formatProviderImagesResponse(created int64, size, quality, outputFormat string, images [][]byte) ([]byte, error) {
	data := make([]llmadapter.ImagesGenerationsDatum, 0, len(images))
	for _, img := range images {
		data = append(data, llmadapter.ImagesGenerationsDatum{B64JSON: base64.StdEncoding.EncodeToString(img)})
	}
	resp := llmadapter.ImagesGenerationsResponse{
		Created:      created,
		Data:         data,
		Size:         size,
		Quality:      quality,
		OutputFormat: outputFormat,
	}
	return json.Marshal(resp)
}

// peekImagesModelFromRequest reads model without consuming unsupported Crynux validation.
func peekImagesModelFromRequest(c *gin.Context) (model string, body []byte, err error) {
	ct := c.ContentType()
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := c.Request.ParseMultipartForm(8 << 20); err != nil {
			return "", nil, err
		}
		model = strings.ToLower(strings.TrimSpace(c.Request.FormValue("model")))
		// Rebuild a JSON body from multipart for the Provider path.
		canonical, err := provider.ParseCanonicalImagesMultipart(c.Request.MultipartForm, models.TaskAPITypeImagesEdits)
		if err != nil {
			return model, nil, err
		}
		body, err = rebuildProviderImagesBody(canonical)
		return model, body, err
	}
	body, err = io.ReadAll(c.Request.Body)
	if err != nil {
		return "", nil, err
	}
	return extractImagesModel(body), body, nil
}
