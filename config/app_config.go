package config

const (
	EnvProduction = "production"
	EnvDebug      = "debug"
	EnvTest       = "test"
)

type TokenConfig struct {
	Address  string `mapstructure:"address"`
	Decimals uint8  `mapstructure:"decimals"`
}

type BlockchainNetworkConfig struct {
	ChainID          uint64                 `mapstructure:"chain_id"`
	RpcEndpoint      string                 `mapstructure:"rpc_endpoint"`
	RPS              uint64                 `mapstructure:"rps"`
	StartBlockNum    uint64                 `mapstructure:"start_block_num"`
	LogBlockRange    uint64                 `mapstructure:"log_block_range"`
	ReceivingAddress string                 `mapstructure:"receiving_address"`
	Tokens           map[string]TokenConfig `mapstructure:"tokens"`
}

type AppConfig struct {
	Environment string `mapstructure:"environment"`

	Db struct {
		Driver           string `mapstructure:"driver"`
		ConnectionString string `mapstructure:"connection"`
		Log              struct {
			Level       string `mapstructure:"level"`
			Output      string `mapstructure:"output"`
			MaxFileSize int    `mapstructure:"max_file_size"`
			MaxDays     int    `mapstructure:"max_days"`
			MaxFileNum  int    `mapstructure:"max_file_num"`
		} `mapstructure:"log"`
	} `mapstructure:"db"`

	Log struct {
		Level       string `mapstructure:"level"`
		Output      string `mapstructure:"output"`
		MaxFileSize int    `mapstructure:"max_file_size"`
		MaxDays     int    `mapstructure:"max_days"`
		MaxFileNum  int    `mapstructure:"max_file_num"`
	} `mapstructure:"log"`

	Http struct {
		Host         string `mapstructure:"host"`
		Port         string `mapstructure:"port"`
		MaxBodyBytes int64  `mapstructure:"max_body_bytes"`

		JWT struct {
			SecretKey     string `mapstructure:"secret_key"`
			SecretKeyFile string `mapstructure:"secret_key_file"`
			ExpiresIn     uint64 `mapstructure:"expires_in"`
		} `mapstructure:"jwt"`
	} `mapstructure:"http"`

	Blockchains map[string]BlockchainNetworkConfig `mapstructure:"blockchains"`

	Bridge struct {
		BaseURL    string `mapstructure:"base_url"`
		APIKey     string `mapstructure:"api_key"`
		APIKeyFile string `mapstructure:"api_key_file"`
	} `mapstructure:"bridge"`
}
