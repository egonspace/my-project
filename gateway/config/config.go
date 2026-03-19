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
		BlockchainRPCURL: "https://api.test.stablenet.network",
		BlockchainWSURL:  "ws://api.test.stablenet.network",
		FiatManagerAddr:  "0xFiatManagerContractAddress",
		FiatTokenAddr:    "0xFiatTokenContractAddress",
		AdminPrivateKey:  "0x08c59f13ba871f16db690f25ade76e37db0609ca294c9e5ae9db58f4ba29b3ed",
		FirmBankingURL:   "http://firmbanking.internal",
	}
}
