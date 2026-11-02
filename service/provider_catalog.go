package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// ErrLoadedModelsFetchFailed marks a Relay loaded-models fetch failure during
// Provider and Crynux name-collision validation.
var ErrLoadedModelsFetchFailed = errors.New("loaded models fetch failed")

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
// Model names are stored under the lowercase canonical form.
func InitProviderCatalog(cfg *config.AppConfig) error {
	catalog := make(map[string]ProviderModelRoute)
	for serviceID, svc := range cfg.Provider.Services {
		provider := models.ServiceProvider(serviceID)
		for _, model := range svc.Models {
			name := config.NormalizeProviderModelName(model.Name)
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

// LookupProviderModel returns the configured Provider route for a model name.
// The lookup key is the lowercase canonical Provider model name.
func LookupProviderModel(modelName string) (ProviderModelRoute, bool) {
	ensureProviderCatalog()
	name := config.NormalizeProviderModelName(modelName)
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

// IsConfiguredProviderModel reports whether modelName is a configured Provider model.
func IsConfiguredProviderModel(modelName string) bool {
	_, ok := LookupProviderModel(modelName)
	return ok
}

// ProviderModelCollidesWithCrynux reports whether a Provider model name matches a
// Crynux loaded-model id in the in-memory LLM or Image caches.
func ProviderModelCollidesWithCrynux(modelName string) bool {
	name := config.NormalizeProviderModelName(modelName)
	if _, ok := GetLoadedLLMModel(name); ok {
		return true
	}
	_, ok := GetLoadedSDModel(name)
	return ok
}

// ValidateProviderCrynuxCollisions fetches the current Relay loaded-models list and
// returns an error when any configured Provider model name appears there.
// Relay fetch failures wrap ErrLoadedModelsFetchFailed so startup can keep the
// existing soft-fail behavior for an unavailable Relay.
func ValidateProviderCrynuxCollisions(ctx context.Context) error {
	llmModelsCache.mu.RLock()
	client := llmModelsCache.client
	llmModelsCache.mu.RUnlock()
	if client == nil {
		return fmt.Errorf("%w: loaded models cache is not initialized", ErrLoadedModelsFetchFailed)
	}

	loadedModels, err := client.GetLoadedModels(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrLoadedModelsFetchFailed, err)
	}

	seen := make(map[string]struct{})
	for _, loaded := range loadedModels {
		id := config.NormalizeProviderModelName(loaded.ModelID)
		if id == "" {
			continue
		}
		if !IsConfiguredProviderModel(id) {
			continue
		}
		seen[id] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Errorf("provider model name(s) collide with Crynux catalog: %s", strings.Join(names, ", "))
}
