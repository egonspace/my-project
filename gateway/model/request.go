package model

import "time"

const MintExpiration = 3 * time.Minute
const BurnExpiration = 3 * time.Minute

const (
	TypeMint = 1
	TypeBurn = 2

	StatusRequested = 1
	StatusPending   = 2
	StatusSuccess   = 3
	StatusFailure   = -1

	ErrorCodeNone            = 0
	ErrorCodeTxFailed        = 1
	ErrorCodeTxTimeout       = 2
	ErrorCodeBankingFailed   = 3
	ErrorCodeDuplicateRequest = 4
)

type Request struct {
	ID         int64  `json:"id"          db:"id"`
	Type       int    `json:"type"        db:"type"`
	Status     int    `json:"status"      db:"status"`
	UserID     string `json:"user_id"     db:"user_id"`
	BankTx     string `json:"bank_tx"     db:"bank_tx"`
	TxHash     string `json:"tx_hash"     db:"tx_hash"`
	Timestamp  int64  `json:"timestamp"   db:"timestamp"`
	Expiration int64  `json:"expiration"  db:"expiration"`
	Amount     int64  `json:"amount"      db:"amount"`
	ErrorCode  int    `json:"error_code"  db:"error_code"`
}
