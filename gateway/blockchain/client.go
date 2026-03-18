package blockchain

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// FiatManager ABI: mintFromFiat 함수 + FiatTokenMinted/FiatTokenBurnt 이벤트
// 실제 컨트랙트 시그니처:
//   mintFromFiat(address _to, uint256 _amount, uint256 _expiration, uint256 _txId)
//   event FiatTokenMinted(uint256 indexed _txId, address indexed _minter, uint256 _amount)
//   event FiatTokenBurnt(uint256 indexed _txId, address indexed _minter, uint256 _amount)
const fiatManagerABIJSON = `[
  {
    "name": "mintFromFiat",
    "type": "function",
    "inputs": [
      {"name": "_to",         "type": "address"},
      {"name": "_amount",     "type": "uint256"},
      {"name": "_expiration", "type": "uint256"},
      {"name": "_txId",       "type": "uint256"}
    ],
    "outputs": []
  },
  {
    "name": "FiatTokenMinted",
    "type": "event",
    "anonymous": false,
    "inputs": [
      {"name": "_txId",   "type": "uint256", "indexed": true},
      {"name": "_minter", "type": "address", "indexed": true},
      {"name": "_amount", "type": "uint256", "indexed": false}
    ]
  },
  {
    "name": "FiatTokenBurnt",
    "type": "event",
    "anonymous": false,
    "inputs": [
      {"name": "_txId",   "type": "uint256", "indexed": true},
      {"name": "_minter", "type": "address", "indexed": true},
      {"name": "_amount", "type": "uint256", "indexed": false}
    ]
  }
]`

type MintEvent struct {
	TxHash      string
	OnChainTxID string // 컨트랙트의 _txId (bankTx의 keccak256 해시)
	To          string
	Amount      int64
	BlockNumber uint64
}

type BurnEvent struct {
	TxHash      string
	OnChainTxID string // 컨트랙트의 _txId
	Burner      string
	Amount      int64
	BlockNumber uint64
}

type Client interface {
	SendMintTx(ctx context.Context, requesterID string, toAddress string, bankTx string, amount int64, expiration int64) (string, error)
	SubscribeMintEvents(ctx context.Context, ch chan<- MintEvent) error
	SubscribeBurnEvents(ctx context.Context, ch chan<- BurnEvent) error
}

type EthClient struct {
	rpcURL          string
	wsURL           string
	fiatManagerAddr common.Address
	privateKey      *ecdsa.PrivateKey
	chainID         *big.Int
	fiatManagerABI  abi.ABI
}

func NewEthClient(rpcURL, wsURL, fiatManagerAddr, _ /* fiatTokenAddr */, privateKeyHex string) (*EthClient, error) {
	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKeyHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	fiatManagerABI, err := abi.JSON(strings.NewReader(fiatManagerABIJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to parse FiatManager ABI: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	httpClient, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}
	defer httpClient.Close()

	chainID, err := httpClient.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get chainID: %w", err)
	}

	return &EthClient{
		rpcURL:          rpcURL,
		wsURL:           wsURL,
		fiatManagerAddr: common.HexToAddress(fiatManagerAddr),
		privateKey:      privateKey,
		chainID:         chainID,
		fiatManagerABI:  fiatManagerABI,
	}, nil
}

// bankTxToTxID는 string 타입의 bankTx를 컨트랙트의 uint256 _txId로 변환합니다.
// keccak256 해시를 사용하여 결정론적이고 충돌 저항성이 있는 uint256을 생성합니다.
func bankTxToTxID(bankTx string) *big.Int {
	hash := crypto.Keccak256([]byte(bankTx))
	return new(big.Int).SetBytes(hash)
}

func (c *EthClient) SendMintTx(ctx context.Context, _ string, toAddress string, bankTx string, amount int64, expiration int64) (string, error) {
	client, err := ethclient.DialContext(ctx, c.rpcURL)
	if err != nil {
		return "", fmt.Errorf("dial failed: %w", err)
	}
	defer client.Close()

	fromAddr := crypto.PubkeyToAddress(c.privateKey.PublicKey)
	toAddr := common.HexToAddress(toAddress)
	txID := bankTxToTxID(bankTx)

	data, err := c.fiatManagerABI.Pack("mintFromFiat", toAddr, big.NewInt(amount), big.NewInt(expiration), txID)
	if err != nil {
		return "", fmt.Errorf("abi pack failed: %w", err)
	}

	nonce, err := client.PendingNonceAt(ctx, fromAddr)
	if err != nil {
		return "", fmt.Errorf("get nonce failed: %w", err)
	}

	gasLimit, err := client.EstimateGas(ctx, ethereum.CallMsg{
		From: fromAddr,
		To:   &c.fiatManagerAddr,
		Data: data,
	})
	if err != nil {
		log.Printf("[Blockchain] EstimateGas failed (using fallback 200000): %v", err)
		gasLimit = 200000
	}

	signedTx, err := c.buildAndSignTx(ctx, client, nonce, &c.fiatManagerAddr, big.NewInt(0), gasLimit, data)
	if err != nil {
		return "", err
	}

	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return "", fmt.Errorf("send tx failed: %w", err)
	}

	txHash := signedTx.Hash().Hex()
	log.Printf("[Blockchain] SendMintTx submitted txHash=%s to=%s amount=%d", txHash, toAddress, amount)
	return txHash, nil
}

func (c *EthClient) buildAndSignTx(ctx context.Context, client *ethclient.Client, nonce uint64, to *common.Address, value *big.Int, gasLimit uint64, data []byte) (*types.Transaction, error) {
	header, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("get header failed: %w", err)
	}

	var tx *types.Transaction
	if header.BaseFee != nil {
		// EIP-1559
		gasTipCap, err := client.SuggestGasTipCap(ctx)
		if err != nil {
			return nil, fmt.Errorf("get gas tip failed: %w", err)
		}
		gasFeeCap := new(big.Int).Add(
			new(big.Int).Mul(header.BaseFee, big.NewInt(2)),
			gasTipCap,
		)
		tx = types.NewTx(&types.DynamicFeeTx{
			ChainID:   c.chainID,
			Nonce:     nonce,
			To:        to,
			Value:     value,
			Gas:       gasLimit,
			GasTipCap: gasTipCap,
			GasFeeCap: gasFeeCap,
			Data:      data,
		})
	} else {
		// Legacy
		gasPrice, err := client.SuggestGasPrice(ctx)
		if err != nil {
			return nil, fmt.Errorf("get gas price failed: %w", err)
		}
		tx = types.NewTx(&types.LegacyTx{
			Nonce:    nonce,
			To:       to,
			Value:    value,
			Gas:      gasLimit,
			GasPrice: gasPrice,
			Data:     data,
		})
	}

	signer := types.LatestSignerForChainID(c.chainID)
	signedTx, err := types.SignTx(tx, signer, c.privateKey)
	if err != nil {
		return nil, fmt.Errorf("sign tx failed: %w", err)
	}
	return signedTx, nil
}

func (c *EthClient) SubscribeMintEvents(ctx context.Context, ch chan<- MintEvent) error {
	topic := c.fiatManagerABI.Events["FiatTokenMinted"].ID
	query := ethereum.FilterQuery{
		Addresses: []common.Address{c.fiatManagerAddr},
		Topics:    [][]common.Hash{{topic}},
	}
	go c.subscribeWithRetry(ctx, query, func(l types.Log) {
		event, err := c.parseMintLog(l)
		if err != nil {
			log.Printf("[Blockchain] parseMintLog error: %v", err)
			return
		}
		ch <- *event
	})
	return nil
}

func (c *EthClient) SubscribeBurnEvents(ctx context.Context, ch chan<- BurnEvent) error {
	topic := c.fiatManagerABI.Events["FiatTokenBurnt"].ID
	query := ethereum.FilterQuery{
		Addresses: []common.Address{c.fiatManagerAddr},
		Topics:    [][]common.Hash{{topic}},
	}
	go c.subscribeWithRetry(ctx, query, func(l types.Log) {
		event, err := c.parseBurnLog(l)
		if err != nil {
			log.Printf("[Blockchain] parseBurnLog error: %v", err)
			return
		}
		ch <- *event
	})
	return nil
}

func (c *EthClient) subscribeWithRetry(ctx context.Context, query ethereum.FilterQuery, handler func(types.Log)) {
	const retryDelay = 5 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.doSubscribe(ctx, query, handler); err != nil {
			log.Printf("[Blockchain] subscription error: %v, retrying in %v", err, retryDelay)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryDelay):
		}
	}
}

func (c *EthClient) doSubscribe(ctx context.Context, query ethereum.FilterQuery, handler func(types.Log)) error {
	wsClient, err := ethclient.DialContext(ctx, c.wsURL)
	if err != nil {
		return fmt.Errorf("ws dial failed: %w", err)
	}
	defer wsClient.Close()

	logsCh := make(chan types.Log, 100)
	sub, err := wsClient.SubscribeFilterLogs(ctx, query, logsCh)
	if err != nil {
		return fmt.Errorf("SubscribeFilterLogs failed: %w", err)
	}
	defer sub.Unsubscribe()

	log.Printf("[Blockchain] event subscription established for FiatManager=%s", c.fiatManagerAddr.Hex())

	for {
		select {
		case err := <-sub.Err():
			return fmt.Errorf("subscription dropped: %w", err)
		case l := <-logsCh:
			if l.Removed {
				continue
			}
			handler(l)
		case <-ctx.Done():
			return nil
		}
	}
}

func (c *EthClient) parseMintLog(l types.Log) (*MintEvent, error) {
	// Topics[0]=event sig, Topics[1]=_txId(uint256), Topics[2]=_minter(address)
	if len(l.Topics) < 3 {
		return nil, fmt.Errorf("FiatTokenMinted log: expected 3 topics, got %d", len(l.Topics))
	}
	var data struct{ Amount *big.Int }
	if err := c.fiatManagerABI.UnpackIntoInterface(&data, "FiatTokenMinted", l.Data); err != nil {
		return nil, fmt.Errorf("unpack FiatTokenMinted data: %w", err)
	}
	txID := new(big.Int).SetBytes(l.Topics[1].Bytes())
	return &MintEvent{
		TxHash:      l.TxHash.Hex(),
		OnChainTxID: txID.String(),
		To:          common.BytesToAddress(l.Topics[2].Bytes()).Hex(),
		Amount:      data.Amount.Int64(),
		BlockNumber: l.BlockNumber,
	}, nil
}

func (c *EthClient) parseBurnLog(l types.Log) (*BurnEvent, error) {
	// Topics[0]=event sig, Topics[1]=_txId(uint256), Topics[2]=_minter(address)
	if len(l.Topics) < 3 {
		return nil, fmt.Errorf("FiatTokenBurnt log: expected 3 topics, got %d", len(l.Topics))
	}
	var data struct{ Amount *big.Int }
	if err := c.fiatManagerABI.UnpackIntoInterface(&data, "FiatTokenBurnt", l.Data); err != nil {
		return nil, fmt.Errorf("unpack FiatTokenBurnt data: %w", err)
	}
	txID := new(big.Int).SetBytes(l.Topics[1].Bytes())
	return &BurnEvent{
		TxHash:      l.TxHash.Hex(),
		OnChainTxID: txID.String(),
		Burner:      common.BytesToAddress(l.Topics[2].Bytes()).Hex(),
		Amount:      data.Amount.Int64(),
		BlockNumber: l.BlockNumber,
	}, nil
}
