package llm

import (
	"context"
	"crynux_as/bridge"
	"crynux_as/config"
	"crynux_as/service"
	"sync"

	"github.com/gin-gonic/gin"
)

var (
	bridgeClientOnce sync.Once
	bridgeClient     *bridge.Client
)

func getBridgeClient() *bridge.Client {
	bridgeClientOnce.Do(func() {
		cfg := config.GetConfig()
		bridgeClient = bridge.NewClient(cfg.Bridge.BaseURL, cfg.Bridge.APIKey)
	})
	return bridgeClient
}

func ChatCompletions(c *gin.Context) {
	client := getBridgeClient()
	handleLLMProxy(
		c,
		service.EstimatePromptTokensFromChatBody,
		func(ctx context.Context, body []byte) (*bridge.Response, error) {
			return client.ChatCompletions(ctx, body)
		},
	)
}

func Completions(c *gin.Context) {
	client := getBridgeClient()
	handleLLMProxy(
		c,
		service.EstimatePromptTokensFromCompletionsBody,
		func(ctx context.Context, body []byte) (*bridge.Response, error) {
			return client.Completions(ctx, body)
		},
	)
}
