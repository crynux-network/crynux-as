package config

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/viper"
)

var appConfig *AppConfig

// InitConfig reads the config from the config file
// and unmarshals it into the AppConfig struct
func InitConfig(configPath string) error {
	v := viper.New()
	v.SetConfigType("yml")
	v.SetConfigName("config")

	if configPath != "" {
		v.AddConfigPath(configPath)
	} else {
		v.AddConfigPath("/app/config")
		v.AddConfigPath("config")
	}

	if err := v.ReadInConfig(); err != nil {
		return err
	}

	appConfig = &AppConfig{}

	if err := v.Unmarshal(appConfig); err != nil {
		return err
	}

	if appConfig.Environment == EnvTest {
		appConfig.Http.JWT.SecretKey = GetTestJWTKey()
		appConfig.Bridge.APIKey = GetTestBridgeAPIKey()
	} else {
		appConfig.Http.JWT.SecretKey = ReadFromFile(appConfig.Http.JWT.SecretKeyFile)
		appConfig.Bridge.APIKey = ReadFromFile(appConfig.Bridge.APIKeyFile)
	}

	if err := checkHttpConfig(); err != nil {
		return err
	}
	if err := checkBlockchainNetworks(); err != nil {
		return err
	}
	if err := checkBridgeConfig(); err != nil {
		return err
	}
	if err := checkRelayConfig(); err != nil {
		return err
	}
	if err := checkLLMConfig(); err != nil {
		return err
	}

	return nil
}

func checkHttpConfig() error {
	if appConfig.Http.MaxBodyBytes <= 0 {
		return errors.New("http.max_body_bytes is not set")
	}
	if appConfig.Http.JWT.ExpiresIn == 0 {
		return errors.New("http.jwt.expires_in is not set")
	}
	return nil
}

func checkBlockchainNetworks() error {
	for network, blockchain := range appConfig.Blockchains {
		if blockchain.RPS == 0 {
			return fmt.Errorf("blockchain %s rps not set", network)
		}
		if blockchain.RpcEndpoint == "" {
			return fmt.Errorf("blockchain %s rpc endpoint not set", network)
		}
		if blockchain.LogBlockRange == 0 {
			return fmt.Errorf("blockchain %s log block range not set", network)
		}
		if blockchain.ScanInterval == 0 {
			return fmt.Errorf("blockchain %s scan interval not set", network)
		}
		if !common.IsHexAddress(blockchain.ReceivingAddress) {
			return fmt.Errorf("blockchain %s receiving address is invalid", network)
		}
		if len(blockchain.Tokens) == 0 {
			return fmt.Errorf("blockchain %s tokens not set", network)
		}
		for token, tokenConfig := range blockchain.Tokens {
			if !common.IsHexAddress(tokenConfig.Address) {
				return fmt.Errorf("blockchain %s token %s address is invalid", network, token)
			}
			if tokenConfig.Decimals == 0 {
				return fmt.Errorf("blockchain %s token %s decimals not set", network, token)
			}
			if tokenConfig.CreditsPerToken == 0 {
				return fmt.Errorf("blockchain %s token %s credits_per_token not set", network, token)
			}
		}
	}
	return nil
}

func checkBridgeConfig() error {
	if strings.TrimSpace(appConfig.Bridge.BaseURL) == "" {
		return errors.New("bridge.base_url is not set")
	}
	if appConfig.Environment != EnvTest && appConfig.Bridge.APIKey == "" {
		return errors.New("bridge api key is not set")
	}
	return nil
}

func checkRelayConfig() error {
	if strings.TrimSpace(appConfig.Relay.BaseURL) == "" {
		return errors.New("relay.base_url is not set")
	}
	return nil
}

func checkLLMConfig() error {
	if appConfig.LLM.DefaultMaxTokens == 0 {
		return errors.New("llm.default_max_tokens is not set")
	}
	if appConfig.LLM.DefaultVramLimit == 0 {
		return errors.New("llm.default_vram_limit is not set")
	}
	if appConfig.LLM.LoadedModelsRefreshInterval == 0 {
		return errors.New("llm.loaded_models_refresh_interval is not set")
	}
	if appConfig.LLM.QueuedPriorityRefreshInterval == 0 {
		return errors.New("llm.queued_priority_refresh_interval is not set")
	}
	if appConfig.LLM.ExecutionTimeCacheTTL == 0 {
		return errors.New("llm.execution_time_cache_ttl is not set")
	}
	if appConfig.LLM.BaseVRAM == 0 {
		return errors.New("llm.base_vram is not set")
	}
	if _, err := appConfig.ParseEmptyQueueMedianPriorityGwei(); err != nil {
		return fmt.Errorf("llm.empty_queue_median_priority_gwei is invalid: %w", err)
	}
	if _, err := appConfig.ParseReferencePriorityGwei(); err != nil {
		return fmt.Errorf("llm.reference_priority_gwei is invalid: %w", err)
	}
	if appConfig.LLM.CreditsPerGwei == 0 {
		return errors.New("llm.credits_per_gwei is not set")
	}
	if appConfig.LLM.MaxTokenRatio < 2 {
		return errors.New("llm.max_token_ratio must be an integer >= 2")
	}
	if appConfig.LLM.JobSubmitTimeout == 0 {
		return errors.New("llm.job_submit_timeout is not set")
	}
	return nil
}

func (cfg *AppConfig) ParseEmptyQueueMedianPriorityGwei() (*big.Int, error) {
	return parsePositiveDecimalGwei(cfg.LLM.EmptyQueueMedianPriorityGwei)
}

func (cfg *AppConfig) ParseReferencePriorityGwei() (*big.Int, error) {
	return parsePositiveDecimalGwei(cfg.LLM.ReferencePriorityGwei)
}

func parsePositiveDecimalGwei(value string) (*big.Int, error) {
	if value == "" {
		return nil, errors.New("must be a positive decimal integer")
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return nil, errors.New("must be a positive decimal integer")
		}
	}
	priority, ok := new(big.Int).SetString(value, 10)
	if !ok || priority.Sign() <= 0 {
		return nil, errors.New("must be a positive decimal integer")
	}
	return priority, nil
}

func ReadFromFile(file string) string {
	b, err := os.ReadFile(file)
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(b))
}

func GetTestJWTKey() string {
	return ""
}

func GetTestBridgeAPIKey() string {
	return ""
}

func GetConfig() *AppConfig {
	return appConfig
}

// SetConfigForTest replaces the process-wide config. Pass nil to clear it.
func SetConfigForTest(cfg *AppConfig) {
	appConfig = cfg
}
