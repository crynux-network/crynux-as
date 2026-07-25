package config

import (
	"errors"
	"fmt"
	"math"
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
	if appConfig.LLM.PromptCreditsPerToken == 0 {
		return errors.New("llm.prompt_credits_per_token is not set")
	}
	if appConfig.LLM.CompletionCreditsPerToken == 0 {
		return errors.New("llm.completion_credits_per_token is not set")
	}
	if appConfig.LLM.DefaultMaxTokens == 0 {
		return errors.New("llm.default_max_tokens is not set")
	}
	if appConfig.LLM.DefaultVramLimit == 0 {
		return errors.New("llm.default_vram_limit is not set")
	}
	if appConfig.LLM.LoadedModelsRefreshInterval == 0 {
		return errors.New("llm.loaded_models_refresh_interval is not set")
	}
	if len(appConfig.LLM.VramRatios) == 0 {
		return errors.New("llm.vram_ratios is not set")
	}
	for i, tier := range appConfig.LLM.VramRatios {
		if tier.MaxVram == 0 {
			return fmt.Errorf("llm.vram_ratios[%d].max_vram is not set", i)
		}
		if i > 0 && tier.MaxVram <= appConfig.LLM.VramRatios[i-1].MaxVram {
			return fmt.Errorf("llm.vram_ratios must be sorted by strictly ascending max_vram at index %d", i)
		}
		if _, err := VramRatioStored(tier.Ratio); err != nil {
			return fmt.Errorf("llm.vram_ratios[%d].ratio is invalid: %w", i, err)
		}
	}
	return nil
}

// VramRatioStored converts a display VRAM ratio to the stored integer (ratio * 10).
func VramRatioStored(ratio float64) (uint, error) {
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio <= 0 {
		return 0, errors.New("must be a positive number")
	}
	stored := uint(math.Round(ratio * 10))
	if stored == 0 || math.Abs(float64(stored)/10.0-ratio) > 1e-9 {
		return 0, errors.New("must have at most one decimal place")
	}
	return stored, nil
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
