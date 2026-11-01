package provider

import (
	"crynux_as/config"
	"crynux_as/models"
	"fmt"
)

type Registry struct {
	adapters map[models.ServiceProvider]Adapter
}

func NewRegistry(cfg *config.AppConfig) (*Registry, error) {
	adapters := make(map[models.ServiceProvider]Adapter)
	defaultMaxTokens := int(cfg.LLM.DefaultMaxTokens)
	for serviceID, svc := range cfg.Provider.Services {
		provider := models.ServiceProvider(serviceID)
		switch provider {
		case models.ServiceProviderOpenAI:
			adapters[provider] = NewOpenAIAdapter(svc, defaultMaxTokens)
		case models.ServiceProviderAnthropic:
			adapters[provider] = NewAnthropicAdapter(svc, defaultMaxTokens)
		case models.ServiceProviderGoogle:
			adapters[provider] = NewGoogleAdapter(svc, defaultMaxTokens)
		default:
			return nil, fmt.Errorf("unknown service provider %s", serviceID)
		}
	}
	return &Registry{adapters: adapters}, nil
}

func (r *Registry) Get(provider models.ServiceProvider) (Adapter, error) {
	adapter, ok := r.adapters[provider]
	if !ok {
		return nil, fmt.Errorf("adapter for %s is not configured", provider)
	}
	return adapter, nil
}
