package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/llmadapter"
	"crynux_as/service/huggingface"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidBaseModel          = errors.New("invalid base model")
	ErrBaseModelCheckUnavailable = errors.New("base model check temporarily unavailable")
)

var (
	hfClientMu sync.Mutex
	hfClient   *huggingface.Client
)

func getHuggingFaceClient() *huggingface.Client {
	hfClientMu.Lock()
	defer hfClientMu.Unlock()
	if hfClient != nil {
		return hfClient
	}
	cfg := config.GetConfig().HuggingFace
	hfClient = huggingface.NewClient(
		cfg.APIBaseURL,
		time.Duration(cfg.TimeoutSeconds*float64(time.Second)),
		time.Duration(cfg.CacheTTLSeconds*float64(time.Second)),
	)
	return hfClient
}

// SetHuggingFaceClientForTest replaces the Hub client used by ValidateTaskModel.
func SetHuggingFaceClientForTest(client *huggingface.Client) {
	hfClientMu.Lock()
	defer hfClientMu.Unlock()
	hfClient = client
}

// ValidateTaskModel checks that the model id is a Hugging Face repo id and that the
// model is either already present in the loaded-models cache or exists on the Hub.
func ValidateTaskModel(ctx context.Context, kind huggingface.ModelKind, modelID, variant string) error {
	modelID = llmadapter.NormalizeModelID(modelID)
	variant = strings.ToLower(strings.TrimSpace(variant))
	if err := llmadapter.ValidateHuggingFaceModelID(modelID); err != nil {
		return errors.Join(ErrInvalidBaseModel, err)
	}
	switch kind {
	case huggingface.ModelKindLLM:
		if _, ok := GetLoadedLLMModel(modelID); ok {
			return nil
		}
	case huggingface.ModelKindSD:
		if _, ok := GetLoadedSDModel(modelID, variant); ok {
			return nil
		}
	default:
		return ErrInvalidBaseModel
	}

	client := getHuggingFaceClient()
	if err := client.ValidateBaseModel(ctx, kind, modelID, variant); err != nil {
		if errors.Is(err, huggingface.ErrServiceUnavailable) {
			return ErrBaseModelCheckUnavailable
		}
		if errors.Is(err, huggingface.ErrVariantUnavailable) {
			return errors.Join(ErrInvalidBaseModel, err)
		}
		if errors.Is(err, huggingface.ErrModelNotFound) {
			return errors.Join(ErrInvalidBaseModel, err)
		}
		if strings.TrimSpace(err.Error()) == "" {
			return ErrInvalidBaseModel
		}
		return errors.Join(ErrInvalidBaseModel, err)
	}
	return nil
}
