package config

type Config struct {
	ServerPort       string
	DatabaseDSN      string
	BlockchainRPCURL string
	BlockchainWSURL  string
	FiatManagerAddr  string
	FiatTokenAddr    string
	AdminPrivateKey  string
	FirmBankingURL   string
}

func Default() *Config {
	return &Config{
		ServerPort:       ":8080",
		DatabaseDSN:      "host=localhost port=5432 user=gateway password=secret dbname=gateway sslmode=disable",
		BlockchainRPCURL: "http://localhost:8545",
		BlockchainWSURL:  "ws://localhost:8546",
		FiatManagerAddr:  "0xFiatManagerContractAddress",
		FiatTokenAddr:    "0xFiatTokenContractAddress",
		AdminPrivateKey:  "0xYourAdminPrivateKey",
		FirmBankingURL:   "http://firmbanking.internal",
	}
}
