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
	if err := checkDBConfig(); err != nil {
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
	if err := checkHuggingFaceConfig(); err != nil {
		return err
	}
	if err := checkTaskSchemaConfig(); err != nil {
		return err
	}
	if err := checkLLMConfig(); err != nil {
		return err
	}
	if err := checkUsageStatsConfig(); err != nil {
		return err
	}
	if err := checkCreditsConfig(v); err != nil {
		return err
	}
	if err := checkProviderConfig(v); err != nil {
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

func checkDBConfig() error {
	if appConfig.Db.Driver == "" {
		return errors.New("db.driver is not set")
	}
	if appConfig.Db.ConnectionString == "" {
		return errors.New("db.connection is not set")
	}
	if appConfig.Db.Driver == "mysql" {
		conn := appConfig.Db.ConnectionString
		if !strings.Contains(conn, "collation=utf8mb4_unicode_ci") {
			return errors.New("db.connection must include collation=utf8mb4_unicode_ci")
		}
		if strings.Contains(conn, "charset=") {
			return errors.New("db.connection must not include charset=; use collation=utf8mb4_unicode_ci only")
		}
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

func checkHuggingFaceConfig() error {
	hf := appConfig.HuggingFace
	if strings.TrimSpace(hf.APIBaseURL) == "" {
		return errors.New("huggingface.api_base_url is not set")
	}
	if hf.TimeoutSeconds <= 0 {
		return errors.New("huggingface.timeout_seconds is not set")
	}
	if hf.CacheTTLSeconds <= 0 {
		return errors.New("huggingface.cache_ttl_seconds is not set")
	}
	return nil
}

func checkTaskSchemaConfig() error {
	schema := appConfig.TaskSchema
	if strings.TrimSpace(schema.StableDiffusionInference) == "" {
		return errors.New("task_schema.stable_diffusion_inference is not set")
	}
	if strings.TrimSpace(schema.GPTInference) == "" {
		return errors.New("task_schema.gpt_inference is not set")
	}
	if strings.TrimSpace(schema.StableDiffusionFinetuneLora) == "" {
		return errors.New("task_schema.stable_diffusion_finetune_lora is not set")
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
	if appConfig.LLM.MinCatalogOnDiskNodeCount == 0 {
		return errors.New("llm.min_catalog_on_disk_node_count is not set")
	}
	if appConfig.LLM.BaseVRAM == 0 {
		return errors.New("llm.base_vram is not set")
	}
	if _, err := appConfig.ParseEmptyQueueMedianPriorityGwei(); err != nil {
		return fmt.Errorf("llm.empty_queue_median_priority_gwei is invalid: %w", err)
	}
	minPriority, err := appConfig.ParseMinPriorityGwei()
	if err != nil {
		return fmt.Errorf("llm.min_priority_gwei is invalid: %w", err)
	}
	maxPriority, err := appConfig.ParseMaxPriorityGwei()
	if err != nil {
		return fmt.Errorf("llm.max_priority_gwei is invalid: %w", err)
	}
	if minPriority.Cmp(maxPriority) > 0 {
		return errors.New("llm.min_priority_gwei must be less than or equal to llm.max_priority_gwei")
	}
	if _, err := appConfig.ParseCreditsPerGwei(); err != nil {
		return fmt.Errorf("llm.credits_per_gwei is invalid: %w", err)
	}
	if _, err := appConfig.MaxTaskFeeGwei(); err != nil {
		return fmt.Errorf("llm.max_task_price_cnx is invalid: %w", err)
	}
	if appConfig.LLM.JobSubmitTimeout == 0 {
		return errors.New("llm.job_submit_timeout is not set")
	}
	if appConfig.LLM.JobRetentionDays == 0 {
		return errors.New("llm.job_retention_days is not set")
	}
	if strings.TrimSpace(appConfig.LLM.TaskResultDir) == "" {
		return errors.New("llm.task_result_dir is not set")
	}
	if appConfig.LLM.ProjectRecentRequestsLimit == 0 {
		return errors.New("llm.project_recent_requests_limit is not set")
	}
	return nil
}

func checkUsageStatsConfig() error {
	if appConfig.UsageStats.RecentFailureWindowSeconds == 0 {
		return errors.New("usage_stats.recent_failure_window_seconds is not set")
	}
	threshold, err := appConfig.ParseRecentFailureRateThreshold()
	if err != nil {
		return fmt.Errorf("usage_stats.recent_failure_rate_threshold is invalid: %w", err)
	}
	one := big.NewRat(1, 1)
	if threshold.Cmp(one) > 0 {
		return errors.New("usage_stats.recent_failure_rate_threshold must be less than or equal to 1")
	}
	return nil
}

func checkCreditsConfig(v *viper.Viper) error {
	if !v.IsSet("credits.signup_bonus") {
		return errors.New("credits.signup_bonus is not set")
	}
	if !v.IsSet("credits.referral.deposit_reward_ratio") {
		return errors.New("credits.referral.deposit_reward_ratio is not set")
	}
	if !v.IsSet("credits.referral.invitee_bonus") {
		return errors.New("credits.referral.invitee_bonus is not set")
	}
	if !v.IsSet("credits.referral.purchase_bonus_tiers") {
		return errors.New("credits.referral.purchase_bonus_tiers is not set")
	}
	ratio, err := appConfig.ParseDepositRewardRatio()
	if err != nil {
		return fmt.Errorf("credits.referral.deposit_reward_ratio is invalid: %w", err)
	}
	one := big.NewRat(1, 1)
	if ratio.Cmp(one) > 0 {
		return errors.New("credits.referral.deposit_reward_ratio must be less than or equal to 1")
	}

	total := new(big.Rat).Set(ratio)
	seenMinPurchases := make(map[uint64]struct{}, len(appConfig.Credits.Referral.PurchaseBonusTiers))
	for i, tier := range appConfig.Credits.Referral.PurchaseBonusTiers {
		if tier.MinPurchases == 0 {
			return fmt.Errorf("credits.referral.purchase_bonus_tiers[%d].min_purchases must be greater than 0", i)
		}
		if _, exists := seenMinPurchases[tier.MinPurchases]; exists {
			return fmt.Errorf("credits.referral.purchase_bonus_tiers[%d].min_purchases is duplicated", i)
		}
		seenMinPurchases[tier.MinPurchases] = struct{}{}

		extra, err := parseNonNegativeRatio(tier.ExtraRatio)
		if err != nil {
			return fmt.Errorf("credits.referral.purchase_bonus_tiers[%d].extra_ratio is invalid: %w", i, err)
		}
		if extra.Cmp(one) > 0 {
			return fmt.Errorf("credits.referral.purchase_bonus_tiers[%d].extra_ratio must be less than or equal to 1", i)
		}
		total.Add(total, extra)
	}
	if total.Cmp(one) > 0 {
		return errors.New("credits.referral.deposit_reward_ratio plus all purchase_bonus_tiers extra_ratio values must be less than or equal to 1")
	}
	return nil
}

func parseNonNegativeRatio(value string) (*big.Rat, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, errors.New("must be a non-negative decimal")
	}
	rate := new(big.Rat)
	if _, ok := rate.SetString(trimmed); !ok {
		return nil, errors.New("must be a non-negative decimal")
	}
	if rate.Sign() < 0 {
		return nil, errors.New("must be a non-negative decimal")
	}
	return rate, nil
}

func parseNonNegativeCNXDecimal(value string) (*big.Int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, errors.New("must be a non-negative decimal CNX string")
	}
	rat := new(big.Rat)
	if _, ok := rat.SetString(trimmed); !ok {
		return nil, errors.New("must be a non-negative decimal CNX string")
	}
	if rat.Sign() < 0 {
		return nil, errors.New("must be a non-negative decimal CNX string")
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	scaled := new(big.Rat).Mul(rat, new(big.Rat).SetInt(scale))
	if !scaled.IsInt() {
		return nil, errors.New("must have at most 18 fractional digits")
	}
	return new(big.Int).Set(scaled.Num()), nil
}

var knownServiceProviders = map[string]struct{}{
	"openai":    {},
	"anthropic": {},
	"google":    {},
}

func checkProviderConfig(v *viper.Viper) error {
	if !v.IsSet("provider") {
		return errors.New("provider is not set")
	}
	p := appConfig.Provider
	if p.MaxConcurrencyPerKey == 0 {
		return errors.New("provider.max_concurrency_per_key must be greater than 0")
	}
	if p.SchedulerIntervalMilliseconds == 0 {
		return errors.New("provider.scheduler_interval_milliseconds must be greater than 0")
	}
	if p.SchedulerBatchSize == 0 {
		return errors.New("provider.scheduler_batch_size must be greater than 0")
	}
	if p.DefaultQueueTimeoutSeconds == 0 {
		return errors.New("provider.default_queue_timeout_seconds must be greater than 0")
	}
	if p.MaxAttemptsPerJob == 0 {
		return errors.New("provider.max_attempts_per_job must be greater than 0")
	}
	if p.KeyCooldownSeconds == 0 {
		return errors.New("provider.key_cooldown_seconds must be greater than 0")
	}
	if p.StreamSubscriberBuffer == 0 {
		return errors.New("provider.stream_subscriber_buffer must be greater than 0")
	}
	if p.ShutdownTimeoutSeconds == 0 {
		return errors.New("provider.shutdown_timeout_seconds must be greater than 0")
	}
	if p.MaxSecretBytes == 0 {
		return errors.New("provider.max_secret_bytes must be greater than 0")
	}
	if strings.TrimSpace(p.KeyEncryption.EncryptionKeyFile) == "" {
		return errors.New("provider.key_encryption.encryption_key_file is not set")
	}
	if len(p.Services) == 0 {
		return errors.New("provider.services is not set")
	}
	modelNames := make(map[string]string)
	for serviceID, svc := range p.Services {
		if _, ok := knownServiceProviders[serviceID]; !ok {
			return fmt.Errorf("provider.services.%s is an unknown service provider", serviceID)
		}
		if strings.TrimSpace(svc.DisplayName) == "" {
			return fmt.Errorf("provider.services.%s.display_name is not set", serviceID)
		}
		if strings.TrimSpace(svc.BaseURL) == "" {
			return fmt.Errorf("provider.services.%s.base_url is not set", serviceID)
		}
		if svc.ConnectTimeoutSeconds == 0 {
			return fmt.Errorf("provider.services.%s.connect_timeout_seconds must be greater than 0", serviceID)
		}
		if svc.ResponseHeaderTimeoutSeconds == 0 {
			return fmt.Errorf("provider.services.%s.response_header_timeout_seconds must be greater than 0", serviceID)
		}
		if svc.StreamIdleTimeoutSeconds == 0 {
			return fmt.Errorf("provider.services.%s.stream_idle_timeout_seconds must be greater than 0", serviceID)
		}
		if svc.RequestTimeoutSeconds == 0 {
			return fmt.Errorf("provider.services.%s.request_timeout_seconds must be greater than 0", serviceID)
		}
		if svc.CredentialValidationTimeoutSeconds == 0 {
			return fmt.Errorf("provider.services.%s.credential_validation_timeout_seconds must be greater than 0", serviceID)
		}
		for i, model := range svc.Models {
			name := strings.TrimSpace(model.Name)
			if name == "" {
				return fmt.Errorf("provider.services.%s.models[%d].name is not set", serviceID, i)
			}
			if other, exists := modelNames[name]; exists {
				return fmt.Errorf("provider model name %q is duplicated between %s and %s", name, other, serviceID)
			}
			modelNames[name] = serviceID
			if err := checkProviderUsageWeights(serviceID, name, model.UsageWeights); err != nil {
				return err
			}
		}
	}
	if !common.IsHexAddress(p.Wallet.SignerAddress) {
		return errors.New("provider.wallet.signer_address is invalid")
	}
	if len(p.Wallet.Networks) == 0 {
		return errors.New("provider.wallet.networks is not set")
	}
	for networkID, network := range p.Wallet.Networks {
		if strings.TrimSpace(network.DisplayName) == "" {
			return fmt.Errorf("provider.wallet.networks.%s.display_name is not set", networkID)
		}
		if !common.IsHexAddress(network.BenefitAddressContract) {
			return fmt.Errorf("provider.wallet.networks.%s.benefit_address_contract is invalid", networkID)
		}
		if !common.IsHexAddress(network.FeeReceiverAddress) {
			return fmt.Errorf("provider.wallet.networks.%s.fee_receiver_address is invalid", networkID)
		}
		if network.MaxWithdrawalsPerDay == 0 {
			return fmt.Errorf("provider.wallet.networks.%s.max_withdrawals_per_day must be greater than 0", networkID)
		}
		if _, err := parseNonNegativeCNXDecimal(network.FixedWithdrawalFeeCNX); err != nil {
			return fmt.Errorf("provider.wallet.networks.%s.fixed_withdrawal_fee_cnx is invalid: %w", networkID, err)
		}
		if _, err := parseNonNegativeCNXDecimal(network.MinimumHotWalletCNX); err != nil {
			return fmt.Errorf("provider.wallet.networks.%s.minimum_hot_wallet_cnx is invalid: %w", networkID, err)
		}
		if _, err := parseNonNegativeCNXDecimal(network.MinimumHotWalletNativeGas); err != nil {
			return fmt.Errorf("provider.wallet.networks.%s.minimum_hot_wallet_native_gas is invalid: %w", networkID, err)
		}
		if _, ok := appConfig.Blockchains[networkID]; !ok {
			return fmt.Errorf("provider.wallet.networks.%s must exist in blockchains", networkID)
		}
		var prevMin *big.Int
		for i, tier := range network.WithdrawalFeeTiers {
			minAmount, err := parseNonNegativeCNXDecimal(tier.MinAmountCNX)
			if err != nil {
				return fmt.Errorf("provider.wallet.networks.%s.withdrawal_fee_tiers[%d].min_amount_cnx is invalid: %w", networkID, i, err)
			}
			if prevMin != nil && minAmount.Cmp(prevMin) <= 0 {
				return fmt.Errorf("provider.wallet.networks.%s.withdrawal_fee_tiers must be strictly increasing by min_amount_cnx", networkID)
			}
			prevMin = minAmount
			if _, err := parseNonNegativeRatio(tier.FeeRatio); err != nil {
				return fmt.Errorf("provider.wallet.networks.%s.withdrawal_fee_tiers[%d].fee_ratio is invalid: %w", networkID, i, err)
			}
		}
	}
	return nil
}

func checkProviderUsageWeights(serviceID, modelName string, weights ProviderUsageWeightsConfig) error {
	fields := map[string]string{
		"input_tokens":             weights.InputTokens,
		"output_tokens":            weights.OutputTokens,
		"cached_input_tokens":      weights.CachedInputTokens,
		"cache_write_input_tokens": weights.CacheWriteInputTokens,
		"reasoning_tokens":         weights.ReasoningTokens,
		"tool_use_prompt_tokens":   weights.ToolUsePromptTokens,
	}
	hasPositive := false
	for field, value := range fields {
		if strings.TrimSpace(value) == "" {
			continue
		}
		rat, err := parseNonNegativeRatio(value)
		if err != nil {
			return fmt.Errorf("provider.services.%s.models[%s].usage_weights.%s is invalid: %w", serviceID, modelName, field, err)
		}
		if rat.Sign() > 0 {
			hasPositive = true
		}
	}
	if !hasPositive {
		return fmt.Errorf("provider.services.%s.models[%s] must have at least one positive usage weight", serviceID, modelName)
	}
	return nil
}

// ParseDepositRewardRatio parses credits.referral.deposit_reward_ratio as a non-negative decimal.
// Zero disables the base deposit referral reward.
func (cfg *AppConfig) ParseDepositRewardRatio() (*big.Rat, error) {
	return parseNonNegativeRatio(cfg.Credits.Referral.DepositRewardRatio)
}

// EffectiveDepositRewardRatio returns base deposit_reward_ratio plus the sum of
// extra_ratio for every purchase_bonus_tier whose min_purchases is at most priorCount.
func (cfg *AppConfig) EffectiveDepositRewardRatio(priorCount uint64) (*big.Rat, error) {
	base, err := cfg.ParseDepositRewardRatio()
	if err != nil {
		return nil, err
	}
	extra, _, err := cfg.PurchaseBonusExtra(priorCount)
	if err != nil {
		return nil, err
	}
	return new(big.Rat).Add(base, extra), nil
}

// PurchaseBonusExtra returns the summed extra_ratio of every purchase_bonus_tier
// whose min_purchases is at most priorCount, and the highest matching
// min_purchases. highestMinPurchases is nil when no tier matches.
func (cfg *AppConfig) PurchaseBonusExtra(priorCount uint64) (*big.Rat, *uint64, error) {
	extra := big.NewRat(0, 1)
	var highest *uint64
	for _, tier := range cfg.Credits.Referral.PurchaseBonusTiers {
		if priorCount < tier.MinPurchases {
			continue
		}
		part, err := parseNonNegativeRatio(tier.ExtraRatio)
		if err != nil {
			return nil, nil, err
		}
		extra.Add(extra, part)
		minPurchases := tier.MinPurchases
		if highest == nil || minPurchases > *highest {
			highest = &minPurchases
		}
	}
	return extra, highest, nil
}

func FormatRatio(rate *big.Rat) string {
	if rate == nil || rate.Sign() == 0 {
		return "0"
	}
	s := rate.FloatString(18)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func (cfg *AppConfig) ParseEmptyQueueMedianPriorityGwei() (*big.Int, error) {
	return parsePositiveDecimalGwei(cfg.LLM.EmptyQueueMedianPriorityGwei)
}

func (cfg *AppConfig) ParseMinPriorityGwei() (*big.Int, error) {
	return parsePositiveDecimalGwei(cfg.LLM.MinPriorityGwei)
}

func (cfg *AppConfig) ParseMaxPriorityGwei() (*big.Int, error) {
	return parsePositiveDecimalGwei(cfg.LLM.MaxPriorityGwei)
}

func (cfg *AppConfig) ParseCreditsPerGwei() (*big.Rat, error) {
	return ParsePositiveDecimalRate(cfg.LLM.CreditsPerGwei)
}

// MaxTaskFeeGwei converts llm.max_task_price_cnx to Gwei as floor(price_cnx * 1e9).
// 1 CNX = 1e18 wei and task_fee_gwei * 1e9 = wei, so 1 CNX = 1e9 Gwei.
func (cfg *AppConfig) MaxTaskFeeGwei() (*big.Int, error) {
	price, err := ParsePositiveDecimalRate(cfg.LLM.MaxTaskPriceCNX)
	if err != nil {
		return nil, err
	}
	scaled := new(big.Rat).Mul(price, big.NewRat(1_000_000_000, 1))
	feeGwei := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	if feeGwei.Sign() <= 0 {
		return nil, errors.New("must convert to a positive Gwei amount")
	}
	return feeGwei, nil
}

func (cfg *AppConfig) ParseRecentFailureRateThreshold() (*big.Rat, error) {
	return ParsePositiveDecimalRate(cfg.UsageStats.RecentFailureRateThreshold)
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

// ParsePositiveDecimalRate parses a positive decimal string (including fractions such as "1/1000000").
func ParsePositiveDecimalRate(value string) (*big.Rat, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, errors.New("must be a positive decimal")
	}
	rate := new(big.Rat)
	if _, ok := rate.SetString(trimmed); !ok {
		return nil, errors.New("must be a positive decimal")
	}
	if rate.Sign() <= 0 {
		return nil, errors.New("must be a positive decimal")
	}
	return rate, nil
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
