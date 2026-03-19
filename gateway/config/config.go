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

	// 로컬 개발용 샘플 유저
	SampleUserID    string
	SampleAddress   string
	SampleAccountNo string
}

func Default() *Config {
	return &Config{
		ServerPort:       ":8080",
		DatabaseDSN:      "host=localhost port=5432 user=gateway password=secret dbname=gateway sslmode=disable",
		BlockchainRPCURL: "https://api.test.stablenet.network",
		BlockchainWSURL:  "wss://ws.test.stablenet.network",
		FiatManagerAddr:  "0xC6fa1EB5532A3eD31872281b214a90332EcF95D2",
		FiatTokenAddr:    "0xcca79c0be6efdFa635839bDDc77B415Cc84B9CbE",
		AdminPrivateKey:  "0x08c59f13ba871f16db690f25ade76e37db0609ca294c9e5ae9db58f4ba29b3ed",
		FirmBankingURL:   "http://localhost:8081",

		SampleUserID:    "sample-user-001",
		SampleAddress:   "0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
		SampleAccountNo: "110-123-456789",
	}
}
