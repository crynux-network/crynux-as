package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// ProviderModelRoute is the immutable routing and pricing snapshot for one configured Provider model.
type ProviderModelRoute struct {
	ServiceProvider models.ServiceProvider
	ModelName       string
	UsageWeights    map[string]string
	DisplayName     string
}

var (
	providerCatalogMu   sync.RWMutex
	providerCatalog     map[string]ProviderModelRoute
	providerCatalogOnce sync.Once
)

// InitProviderCatalog builds the in-memory Provider model map from configuration.
func InitProviderCatalog(cfg *config.AppConfig) error {
	catalog := make(map[string]ProviderModelRoute)
	for serviceID, svc := range cfg.Provider.Services {
		provider := models.ServiceProvider(serviceID)
		for _, model := range svc.Models {
			name := strings.TrimSpace(model.Name)
			weights := map[string]string{}
			addWeight := func(key, value string) {
				if strings.TrimSpace(value) == "" {
					return
				}
				weights[key] = strings.TrimSpace(value)
			}
			addWeight("input_tokens", model.UsageWeights.InputTokens)
			addWeight("output_tokens", model.UsageWeights.OutputTokens)
			addWeight("cached_input_tokens", model.UsageWeights.CachedInputTokens)
			addWeight("cache_write_input_tokens", model.UsageWeights.CacheWriteInputTokens)
			addWeight("reasoning_tokens", model.UsageWeights.ReasoningTokens)
			addWeight("tool_use_prompt_tokens", model.UsageWeights.ToolUsePromptTokens)
			catalog[name] = ProviderModelRoute{
				ServiceProvider: provider,
				ModelName:       name,
				UsageWeights:    weights,
				DisplayName:     svc.DisplayName,
			}
		}
	}
	providerCatalogMu.Lock()
	providerCatalog = catalog
	providerCatalogMu.Unlock()
	return nil
}

func ensureProviderCatalog() {
	providerCatalogOnce.Do(func() {
		if providerCatalog == nil {
			_ = InitProviderCatalog(config.GetConfig())
		}
	})
}

// LookupProviderModel returns the configured Provider route for an exact trimmed model name.
func LookupProviderModel(modelName string) (ProviderModelRoute, bool) {
	ensureProviderCatalog()
	name := strings.TrimSpace(modelName)
	providerCatalogMu.RLock()
	defer providerCatalogMu.RUnlock()
	route, ok := providerCatalog[name]
	return route, ok
}

// ListConfiguredProviderModels returns all configured Provider model routes.
func ListConfiguredProviderModels() []ProviderModelRoute {
	ensureProviderCatalog()
	providerCatalogMu.RLock()
	defer providerCatalogMu.RUnlock()
	out := make([]ProviderModelRoute, 0, len(providerCatalog))
	for _, route := range providerCatalog {
		out = append(out, route)
	}
	return out
}

// ProviderHasEnabledKey reports whether the Service Provider has at least one enabled, non-deleted Shared API Key.
func ProviderHasEnabledKey(ctx context.Context, db *gorm.DB, provider models.ServiceProvider) (bool, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var count int64
	err := db.WithContext(dbCtx).Model(&models.SharedAPIKey{}).
		Where("service_provider = ? AND owner_enabled = ? AND deleted_at IS NULL", provider, true).
		Limit(1).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ListVisibleProviderModels returns configured Provider models whose Service Provider has an enabled key.
func ListVisibleProviderModels(ctx context.Context, db *gorm.DB) ([]ProviderModelRoute, error) {
	routes := ListConfiguredProviderModels()
	visible := make([]ProviderModelRoute, 0, len(routes))
	checked := make(map[models.ServiceProvider]bool)
	for _, route := range routes {
		ok, seen := checked[route.ServiceProvider]
		if !seen {
			var err error
			ok, err = ProviderHasEnabledKey(ctx, db, route.ServiceProvider)
			if err != nil {
				return nil, err
			}
			checked[route.ServiceProvider] = ok
		}
		if ok {
			visible = append(visible, route)
		}
	}
	return visible, nil
}

// ProviderModelCollidesWithCrynux reports whether a Provider model name matches a Crynux catalog id.
func ProviderModelCollidesWithCrynux(modelName string) bool {
	_, ok := GetLoadedLLMModel(modelName)
	return ok
}
