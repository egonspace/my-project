package firmbanking

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
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

// ─── HttpClient: 실제 FirmBanking 서버로 HTTP 요청 ────────────────────────

type HttpClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewHttpClient(baseURL string) *HttpClient {
	return &HttpClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *HttpClient) Transfer(req *TransferRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("firmbanking: marshal error: %w", err)
	}

	resp, err := c.httpClient.Post(c.baseURL+"/transfer", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("firmbanking: request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("firmbanking: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("firmbanking: decode error: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("firmbanking: transfer failed: %s", result.Message)
	}

	return nil
}

// ─── StubClient: 로컬 로깅만 (HTTP 서버 없이 테스트용) ───────────────────

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
