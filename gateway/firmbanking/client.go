package firmbanking

import (
	"fmt"
	"log"
)

type DepositNotification struct {
	BankTx    string `json:"bank_tx"`
	AccountNo string `json:"account_no"`
	Amount    int64  `json:"amount"`
}

type TransferRequest struct {
	ToAccountNo string `json:"to_account_no"`
	Amount      int64  `json:"amount"`
	RefID       string `json:"ref_id"`
}

type Client interface {
	Transfer(req *TransferRequest) error
}

type StubClient struct {
	baseURL string
}

func NewStubClient(baseURL string) *StubClient {
	return &StubClient{baseURL: baseURL}
}

func (c *StubClient) Transfer(req *TransferRequest) error {
	log.Printf("[FirmBanking] Transfer to=%s amount=%d refID=%s",
		req.ToAccountNo, req.Amount, req.RefID)

	if req.Amount <= 0 {
		return fmt.Errorf("invalid amount: %d", req.Amount)
	}
	if req.ToAccountNo == "" {
		return fmt.Errorf("to_account_no is empty")
	}

	log.Printf("[FirmBanking] Transfer SUCCESS to=%s amount=%d", req.ToAccountNo, req.Amount)
	return nil
}
