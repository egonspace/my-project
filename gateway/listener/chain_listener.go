package listener

import (
	"context"
	"errors"
	"fmt"
	"gateway/blockchain"
	"gateway/db"
	"gateway/firmbanking"
	"gateway/model"
	"log"
	"time"
)

// listenerStateKey는 listener_state 테이블에서 FiatManager 리스너의 블록 진행 상황을 식별하는 키
const listenerStateKey = "fiat_manager"

type ChainListener struct {
	stateDB     *db.StateDB
	blockchain  blockchain.Client
	firmBanking firmbanking.Client
}

func NewChainListener(stateDB *db.StateDB, bc blockchain.Client, fb firmbanking.Client) *ChainListener {
	return &ChainListener{
		stateDB:     stateDB,
		blockchain:  bc,
		firmBanking: fb,
	}
}

func (l *ChainListener) Start(ctx context.Context) error {
	// 재기동 시 마지막으로 처리 완료한 블록 이후부터 스캔
	lastBlock, err := l.stateDB.GetLastBlock(listenerStateKey)
	if err != nil {
		return fmt.Errorf("GetLastBlock failed: %w", err)
	}
	if lastBlock > 0 {
		log.Printf("[ChainListener] resuming from block %d", lastBlock)
	}

	mintCh := make(chan blockchain.MintEvent, 100)
	burnCh := make(chan blockchain.BurnEvent, 100)

	if err := l.blockchain.SubscribeMintEvents(ctx, mintCh, lastBlock); err != nil {
		return fmt.Errorf("SubscribeMintEvents failed: %w", err)
	}
	if err := l.blockchain.SubscribeBurnEvents(ctx, burnCh, lastBlock); err != nil {
		return fmt.Errorf("SubscribeBurnEvents failed: %w", err)
	}

	log.Println("[ChainListener] started")

	go l.listenMintEvents(ctx, mintCh)
	go l.listenBurnEvents(ctx, burnCh)
	go l.runRetryPoller(ctx)

	return nil
}

func (l *ChainListener) listenMintEvents(ctx context.Context, ch <-chan blockchain.MintEvent) {
	for {
		select {
		case <-ctx.Done():
			log.Println("[ChainListener] listenMintEvents stopped")
			return
		case event, ok := <-ch:
			if !ok {
				log.Println("[ChainListener] mintCh closed")
				return
			}
			l.handleMintEvent(event)
		}
	}
}

func (l *ChainListener) listenBurnEvents(ctx context.Context, ch <-chan blockchain.BurnEvent) {
	for {
		select {
		case <-ctx.Done():
			log.Println("[ChainListener] listenBurnEvents stopped")
			return
		case event, ok := <-ch:
			if !ok {
				log.Println("[ChainListener] burnCh closed")
				return
			}
			l.handleBurnEvent(event)
		}
	}
}

func (l *ChainListener) handleMintEvent(event blockchain.MintEvent) {
	log.Printf("[ChainListener] MintEvent received txHash=%s amount=%d", event.TxHash, event.Amount)

	record, err := l.stateDB.GetRequestByTxHash(event.TxHash)
	if err != nil {
		log.Printf("[ChainListener] handleMintEvent GetRequestByTxHash error: %v", err)
		return
	}
	if record == nil {
		log.Printf("[ChainListener] handleMintEvent no request found for txHash=%s", event.TxHash)
		l.saveLastBlock(event.BlockNumber)
		return
	}
	if record.Status == model.StatusSuccess {
		log.Printf("[ChainListener] handleMintEvent already succeeded id=%d", record.ID)
		l.saveLastBlock(event.BlockNumber)
		return
	}

	record.Status = model.StatusSuccess
	if err := l.stateDB.UpdateRequest(record); err != nil {
		log.Printf("[ChainListener] handleMintEvent UpdateRequest error: %v", err)
		return
	}
	log.Printf("[ChainListener] MintEvent confirmed id=%d txHash=%s status=SUCCESS", record.ID, event.TxHash)
	l.saveLastBlock(event.BlockNumber)
}

const retryPollInterval = 10 * time.Second

func (l *ChainListener) runRetryPoller(ctx context.Context) {
	ticker := time.NewTicker(retryPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("[ChainListener] retryPoller stopped")
			return
		case <-ticker.C:
			l.timeoutStaleRequests()
		}
	}
}

func (l *ChainListener) timeoutStaleRequests() {
	now := time.Now().Unix()
	records, err := l.stateDB.GetStaleMintRequests(now)
	if err != nil {
		log.Printf("[ChainListener] GetStaleMintRequests error: %v", err)
		return
	}
	if len(records) == 0 {
		return
	}
	log.Printf("[ChainListener] retryPoller found %d timed-out mint request(s)", len(records))

	for _, record := range records {
		record.Status = model.StatusFailure
		record.ErrorCode = model.ErrorCodeTxTimeout
		if err := l.stateDB.UpdateRequest(record); err != nil {
			log.Printf("[ChainListener] retryPoller UpdateRequest failed id=%d: %v", record.ID, err)
			continue
		}
		log.Printf("[ChainListener] retryPoller timed out id=%d", record.ID)
	}
}

func (l *ChainListener) handleBurnEvent(event blockchain.BurnEvent) {
	log.Printf("[ChainListener] BurnEvent received txHash=%s burner=%s amount=%d",
		event.TxHash, event.Burner, event.Amount)

	user, err := l.stateDB.GetUserByAddress(event.Burner)
	if err != nil {
		log.Printf("[ChainListener] handleBurnEvent GetUserByAddress error: %v", err)
		return
	}
	if user == nil {
		log.Printf("[ChainListener] handleBurnEvent no user found for address=%s", event.Burner)
		return
	}

	record := &model.Request{
		Type:      model.TypeBurn,
		Status:    model.StatusRequested,
		UserID:    user.UserID,
		TxHash:    event.TxHash,
		Amount:    event.Amount,
		Timestamp: time.Now().UnixMilli(),
	}
	id, err := l.stateDB.InsertRequest(record)
	if err != nil {
		if errors.Is(err, db.ErrDuplicateTxHash) {
			log.Printf("[ChainListener] handleBurnEvent duplicate txHash=%s, skipping", event.TxHash)
			l.saveLastBlock(event.BlockNumber)
			return
		}
		log.Printf("[ChainListener] handleBurnEvent InsertRequest error: %v", err)
		return
	}
	record.ID = id
	log.Printf("[ChainListener] BurnEvent inserted request id=%d user_id=%s status=REQUESTED", id, user.UserID)

	transferReq := &firmbanking.TransferRequest{
		ToAccountNo: user.AccountNo,
		Amount:      event.Amount,
		RefID:       event.TxHash,
	}
	// firmBanking.Transfer는 동기방식으로 트랜잭셔널하게 처리된다고 가정
	// 만약 비동기로 진행된다면 Status의 세분화 필요함 (예: PENDING -> SUCCESS/FAILURE)
	if err := l.firmBanking.Transfer(transferReq); err != nil {
		log.Printf("[ChainListener] handleBurnEvent Transfer error: %v", err)
		record.Status = model.StatusFailure
		record.ErrorCode = model.ErrorCodeBankingFailed
		_ = l.stateDB.UpdateRequest(record)
		return
	}

	record.Status = model.StatusSuccess
	if err := l.stateDB.UpdateRequest(record); err != nil {
		log.Printf("[ChainListener] handleBurnEvent UpdateRequest error: %v", err)
		return
	}
	log.Printf("[ChainListener] BurnEvent completed id=%d status=SUCCESS", id)
	l.saveLastBlock(event.BlockNumber)
}

func (l *ChainListener) saveLastBlock(blockNumber uint64) {
	if err := l.stateDB.UpsertLastBlock(listenerStateKey, blockNumber); err != nil {
		log.Printf("[ChainListener] saveLastBlock error block=%d: %v", blockNumber, err)
	}
}
