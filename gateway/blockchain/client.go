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
//
//	mintFromFiat(address _to, uint256 _amount, uint256 _expiration, uint256 _txId)
//	event FiatTokenMinted(uint256 indexed _txId, address indexed _minter, uint256 _amount)
//	event FiatTokenBurnt(uint256 indexed _txId, address indexed _minter, uint256 _amount)
const fiatManagerABIJSON = `[
  {
    "name": "mintFromFiat",
    "type": "function",
    "inputs": [
      {"name": "_to",         "type": "address"},
      {"name": "_amount",     "type": "uint256"},
      {"name": "_expiration", "type": "uint256"},
      {"name": "_txId",       "type": "bytes"}
    ],
    "outputs": []
  },
  {
    "name": "FiatTokenMinted",
    "type": "event",
    "anonymous": false,
    "inputs": [
      {"name": "_minter", "type": "address", "indexed": true},
      {"name": "_txId",   "type": "bytes",   "indexed": false},
      {"name": "_amount", "type": "uint256", "indexed": false}
    ]
  },
  {
    "name": "FiatTokenBurnt",
    "type": "event",
    "anonymous": false,
    "inputs": [
      {"name": "_minter", "type": "address", "indexed": true},
      {"name": "_amount", "type": "uint256", "indexed": false}
    ]
  }
]`

// FiatToken의 decimals() 조회에만 사용하는 최소 ABI
const fiatTokenABIJSON = `[
  {
    "name": "decimals",
    "type": "function",
    "inputs": [],
    "outputs": [{"name": "", "type": "uint8"}]
  }
]`

type MintEvent struct {
	TxHash      string
	OnChainTxID string // 컨트랙트의 _txId (bankTx의 keccak256 해시)
	To          string
	Amount      int64 // 원(KRW) 단위 — decimal 역변환 완료
	BlockNumber uint64
}

type BurnEvent struct {
	TxHash      string
	Burner      string
	Amount      int64 // 원(KRW) 단위 — decimal 역변환 완료
	BlockNumber uint64
}

type Client interface {
	SendMintTx(ctx context.Context, requesterID string, toAddress string, bankTx string, amount int64, expiration int64) (string, error)
	// fromBlock: 0이면 현재 시점부터 구독, >0이면 해당 블록부터 히스토리 스캔 후 구독
	SubscribeMintEvents(ctx context.Context, ch chan<- MintEvent, fromBlock uint64) error
	SubscribeBurnEvents(ctx context.Context, ch chan<- BurnEvent, fromBlock uint64) error
}

type EthClient struct {
	rpcURL            string
	wsURL             string
	fiatManagerAddr   common.Address
	privateKey        *ecdsa.PrivateKey
	chainID           *big.Int
	fiatManagerABI    abi.ABI
	decimalMultiplier *big.Int // 10^tokenDecimal
}

func NewEthClient(rpcURL, wsURL, fiatManagerAddr, fiatTokenAddr, privateKeyHex string) (*EthClient, error) {
	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKeyHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	fiatManagerABI, err := abi.JSON(strings.NewReader(fiatManagerABIJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to parse FiatManager ABI: %w", err)
	}

	fiatTokenABI, err := abi.JSON(strings.NewReader(fiatTokenABIJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to parse FiatToken ABI: %w", err)
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

	// FiatToken 컨트랙트에서 decimal 조회
	tokenAddr := common.HexToAddress(fiatTokenAddr)
	decimalMultiplier, err := fetchDecimalMultiplier(ctx, httpClient, tokenAddr, fiatTokenABI)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch token decimals: %w", err)
	}
	log.Printf("[Blockchain] FiatToken decimals fetched: multiplier=%s", decimalMultiplier.String())

	return &EthClient{
		rpcURL:            rpcURL,
		wsURL:             wsURL,
		fiatManagerAddr:   common.HexToAddress(fiatManagerAddr),
		privateKey:        privateKey,
		chainID:           chainID,
		fiatManagerABI:    fiatManagerABI,
		decimalMultiplier: decimalMultiplier,
	}, nil
}

// fetchDecimalMultiplier는 토큰 컨트랙트의 decimals()를 호출해 10^decimal 을 반환
func fetchDecimalMultiplier(ctx context.Context, client *ethclient.Client, tokenAddr common.Address, tokenABI abi.ABI) (*big.Int, error) {
	callData, err := tokenABI.Pack("decimals")
	if err != nil {
		return nil, fmt.Errorf("pack decimals call: %w", err)
	}

	result, err := client.CallContract(ctx, ethereum.CallMsg{
		To:   &tokenAddr,
		Data: callData,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("call decimals: %w", err)
	}

	vals, err := tokenABI.Unpack("decimals", result)
	if err != nil {
		return nil, fmt.Errorf("unpack decimals: %w", err)
	}
	if len(vals) == 0 {
		return nil, fmt.Errorf("decimals returned empty result")
	}

	decimal, ok := vals[0].(uint8)
	if !ok {
		return nil, fmt.Errorf("decimals: unexpected type %T", vals[0])
	}

	multiplier := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimal)), nil)
	return multiplier, nil
}

// toContractAmount는 원(KRW) 단위 금액을 컨트랙트 토큰 단위로 변환
// contractAmount = bankAmount × 10^decimal
func (c *EthClient) toContractAmount(bankAmount int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(bankAmount), c.decimalMultiplier)
}

// toBankAmount는 컨트랙트 토큰 단위 금액을 원(KRW) 단위로 역변환
// bankAmount = contractAmount / 10^decimal
func (c *EthClient) toBankAmount(contractAmount *big.Int) int64 {
	return new(big.Int).Div(contractAmount, c.decimalMultiplier).Int64()
}

func (c *EthClient) SendMintTx(ctx context.Context, _ string, toAddress string, bankTx string, amount int64, expiration int64) (string, error) {
	client, err := ethclient.DialContext(ctx, c.rpcURL)
	if err != nil {
		return "", fmt.Errorf("dial failed: %w", err)
	}
	defer client.Close()

	fromAddr := crypto.PubkeyToAddress(c.privateKey.PublicKey)
	toAddr := common.HexToAddress(toAddress)
	contractAmount := c.toContractAmount(amount) // 원 → 토큰 단위

	data, err := c.fiatManagerABI.Pack("mintFromFiat", toAddr, contractAmount, big.NewInt(expiration), []byte(bankTx))
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
	log.Printf("[Blockchain] SendMintTx submitted txHash=%s to=%s amount=%d (contractAmount=%s)", txHash, toAddress, amount, contractAmount.String())
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

func (c *EthClient) SubscribeMintEvents(ctx context.Context, ch chan<- MintEvent, fromBlock uint64) error {
	topic := c.fiatManagerABI.Events["FiatTokenMinted"].ID
	query := ethereum.FilterQuery{
		Addresses: []common.Address{c.fiatManagerAddr},
		Topics:    [][]common.Hash{{topic}},
	}
	go c.subscribeWithRetry(ctx, query, fromBlock, func(l types.Log) {
		event, err := c.parseMintLog(l)
		if err != nil {
			log.Printf("[Blockchain] parseMintLog error: %v", err)
			return
		}
		ch <- *event
	})
	return nil
}

func (c *EthClient) SubscribeBurnEvents(ctx context.Context, ch chan<- BurnEvent, fromBlock uint64) error {
	topic := c.fiatManagerABI.Events["FiatTokenBurnt"].ID
	query := ethereum.FilterQuery{
		Addresses: []common.Address{c.fiatManagerAddr},
		Topics:    [][]common.Hash{{topic}},
	}
	go c.subscribeWithRetry(ctx, query, fromBlock, func(l types.Log) {
		event, err := c.parseBurnLog(l)
		if err != nil {
			log.Printf("[Blockchain] parseBurnLog error: %v", err)
			return
		}
		ch <- *event
	})
	return nil
}

func (c *EthClient) subscribeWithRetry(ctx context.Context, query ethereum.FilterQuery, fromBlock uint64, handler func(types.Log)) {
	const retryDelay = 5 * time.Second

	// currentFromBlock은 고루틴 내에서만 접근하므로 별도 동기화 불필요
	currentFromBlock := fromBlock

	// 로그 처리 후 다음 재연결 시 재개 지점을 갱신하는 래퍼
	tracked := func(l types.Log) {
		handler(l)
		if next := l.BlockNumber + 1; next > currentFromBlock {
			currentFromBlock = next
		}
	}

	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.doSubscribe(ctx, query, currentFromBlock, tracked); err != nil {
			log.Printf("[Blockchain] subscription error: %v, retrying in %v", err, retryDelay)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryDelay):
		}
	}
}

func (c *EthClient) doSubscribe(ctx context.Context, query ethereum.FilterQuery, fromBlock uint64, handler func(types.Log)) error {
	wsClient, err := ethclient.DialContext(ctx, c.wsURL)
	if err != nil {
		return fmt.Errorf("ws dial failed: %w", err)
	}
	defer wsClient.Close()

	// 라이브 구독을 먼저 시작해 히스토리 스캔과의 사이에서 이벤트가 누락되지 않도록 함
	liveCh := make(chan types.Log, 100)
	sub, err := wsClient.SubscribeFilterLogs(ctx, query, liveCh)
	if err != nil {
		return fmt.Errorf("SubscribeFilterLogs failed: %w", err)
	}
	defer sub.Unsubscribe()

	// fromBlock > 0 이면 마지막 처리 블록 이후부터 현재 블록까지 히스토리 스캔
	if fromBlock > 0 {
		header, err := wsClient.HeaderByNumber(ctx, nil)
		if err != nil {
			return fmt.Errorf("get latest block failed: %w", err)
		}
		latestBlock := header.Number.Uint64()

		if fromBlock <= latestBlock {
			log.Printf("[Blockchain] scanning historical logs block %d → %d", fromBlock, latestBlock)
			httpClient, err := ethclient.DialContext(ctx, c.rpcURL)
			if err != nil {
				return fmt.Errorf("http dial for history failed: %w", err)
			}
			histQuery := query
			histQuery.FromBlock = new(big.Int).SetUint64(fromBlock)
			histQuery.ToBlock = new(big.Int).SetUint64(latestBlock)
			logs, err := httpClient.FilterLogs(ctx, histQuery)
			httpClient.Close()
			if err != nil {
				// 히스토리 스캔 실패는 치명적이지 않음 — 경고 후 라이브 이벤트만 처리
				log.Printf("[Blockchain] FilterLogs (historical) error: %v", err)
			} else {
				log.Printf("[Blockchain] replaying %d historical log(s)", len(logs))
				for _, l := range logs {
					if !l.Removed {
						handler(l)
					}
				}
			}
		}
	}

	log.Printf("[Blockchain] live subscription active for FiatManager=%s (nextBlock=%d)", c.fiatManagerAddr.Hex(), fromBlock)

	for {
		select {
		case err := <-sub.Err():
			return fmt.Errorf("subscription dropped: %w", err)
		case l := <-liveCh:
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
	// Topics[0]=event sig, Topics[1]=_minter(address, indexed)
	// Data = ABI-encoded [_txId(bytes), _amount(uint256)]
	if len(l.Topics) < 2 {
		return nil, fmt.Errorf("FiatTokenMinted log: expected 2 topics, got %d", len(l.Topics))
	}
	var eventData struct {
		TxId   []byte   `abi:"_txId"`
		Amount *big.Int `abi:"_amount"`
	}
	if err := c.fiatManagerABI.UnpackIntoInterface(&eventData, "FiatTokenMinted", l.Data); err != nil {
		return nil, fmt.Errorf("unpack FiatTokenMinted data: %w", err)
	}
	return &MintEvent{
		TxHash:      l.TxHash.Hex(),
		OnChainTxID: string(eventData.TxId), // bytes → 원본 bankTx 문자열
		To:          common.BytesToAddress(l.Topics[1].Bytes()).Hex(),
		Amount:      c.toBankAmount(eventData.Amount), // 토큰 단위 → 원(KRW)
		BlockNumber: l.BlockNumber,
	}, nil
}

func (c *EthClient) parseBurnLog(l types.Log) (*BurnEvent, error) {
	// Topics[0]=event sig, Topics[1]=_minter(address, indexed)
	// Data = ABI-encoded [_amount(uint256)]
	if len(l.Topics) < 2 {
		return nil, fmt.Errorf("FiatTokenBurnt log: expected 2 topics, got %d", len(l.Topics))
	}
	var eventData struct {
		Amount *big.Int `abi:"_amount"`
	}
	if err := c.fiatManagerABI.UnpackIntoInterface(&eventData, "FiatTokenBurnt", l.Data); err != nil {
		return nil, fmt.Errorf("unpack FiatTokenBurnt data: %w", err)
	}
	return &BurnEvent{
		TxHash:      l.TxHash.Hex(),
		Burner:      common.BytesToAddress(l.Topics[1].Bytes()).Hex(),
		Amount:      c.toBankAmount(eventData.Amount), // 토큰 단위 → 원(KRW)
		BlockNumber: l.BlockNumber,
	}, nil
}
