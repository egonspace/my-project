package blockchain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"
)

type MintEvent struct {
	TxHash      string
	Minter      string
	To          string
	Amount      int64
	BlockNumber uint64
}

type BurnEvent struct {
	TxHash      string
	Burner      string
	Amount      int64
	BlockNumber uint64
}

type Client interface {
	SendMintTx(ctx context.Context, requesterID string, toAddress string, amount int64, expiration int64) (string, error)
	SubscribeMintEvents(ctx context.Context, ch chan<- MintEvent) error
	SubscribeBurnEvents(ctx context.Context, ch chan<- BurnEvent) error
}

type StubClient struct {
	rpcURL          string
	fiatManagerAddr string
	adminPrivateKey string
}

func NewStubClient(rpcURL, fiatManagerAddr, adminPrivateKey string) *StubClient {
	return &StubClient{
		rpcURL:          rpcURL,
		fiatManagerAddr: fiatManagerAddr,
		adminPrivateKey: adminPrivateKey,
	}
}

func (c *StubClient) SendMintTx(ctx context.Context, requesterID string, toAddress string, amount int64, expiration int64) (string, error) {
	log.Printf("[Blockchain] SendMintTx requesterID=%s to=%s amount=%d expiration=%d",
		requesterID, toAddress, amount, expiration)

	if amount <= 0 {
		return "", fmt.Errorf("invalid amount: %d", amount)
	}
	if toAddress == "" {
		return "", fmt.Errorf("toAddress is empty")
	}

	txHash, err := randomHex(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate txHash: %w", err)
	}

	log.Printf("[Blockchain] SendMintTx submitted txHash=0x%s", txHash)
	return "0x" + txHash, nil
}

func (c *StubClient) SubscribeMintEvents(ctx context.Context, ch chan<- MintEvent) error {
	log.Printf("[Blockchain] SubscribeMintEvents started, fiatManager=%s", c.fiatManagerAddr)

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("[Blockchain] SubscribeMintEvents stopped")
				return
			case <-ticker.C:
				log.Println("[Blockchain] SubscribeMintEvents polling... (stub: no events)")
			}
		}
	}()

	return nil
}

func (c *StubClient) SubscribeBurnEvents(ctx context.Context, ch chan<- BurnEvent) error {
	log.Printf("[Blockchain] SubscribeBurnEvents started, fiatManager=%s", c.fiatManagerAddr)

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("[Blockchain] SubscribeBurnEvents stopped")
				return
			case <-ticker.C:
				log.Println("[Blockchain] SubscribeBurnEvents polling... (stub: no events)")
			}
		}
	}()

	return nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
