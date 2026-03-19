package api

import (
	"context"
	"encoding/hex"
	"errors"
	"gateway/blockchain"
	"gateway/db"
	"gateway/model"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	stateDB    *db.StateDB
	blockchain blockchain.Client
}

func NewHandler(stateDB *db.StateDB, bc blockchain.Client) *Handler {
	return &Handler{
		stateDB:    stateDB,
		blockchain: bc,
	}
}

type DepositRequest struct {
	UserID string `json:"user_id" binding:"required"`
	BankTx string `json:"bank_tx" binding:"required"`
	Amount int64  `json:"amount"  binding:"required"`
}

func (h *Handler) HandleDeposit(c *gin.Context) {
	var req DepositRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount must be greater than 0"})
		return
	}

	user, err := h.stateDB.GetUserByID(req.UserID)
	if err != nil {
		log.Printf("[HandleDeposit] GetUserByID error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if user == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user not found"})
		return
	}

	record := &model.Request{
		Type:      model.TypeMint,
		Status:    model.StatusRequested,
		UserID:    user.UserID,
		BankTx:    req.BankTx,
		Amount:    req.Amount,
		Timestamp: time.Now().UnixMilli(),
	}
	id, err := h.stateDB.InsertRequest(record)
	if err != nil {
		if errors.Is(err, db.ErrDuplicateBankTx) {
			log.Printf("[HandleDeposit] duplicate bank_tx=%s", req.BankTx)
			c.JSON(http.StatusConflict, gin.H{"error": "duplicate bank_tx"})
			return
		}
		log.Printf("[HandleDeposit] InsertRequest error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	record.ID = id
	log.Printf("[HandleDeposit] inserted request id=%d user_id=%s status=REQUESTED", id, user.UserID)

	expiration := time.Now().Add(model.MintExpiration).Unix()
	txHash, err := h.blockchain.SendMintTx(context.Background(), user.UserID, user.Address, req.BankTx, req.Amount, expiration)
	if err != nil {
		log.Printf("[HandleDeposit] SendMintTx error: %v", err)
		record.Status = model.StatusFailure
		record.ErrorCode = model.ErrorCodeTxFailed
		_ = h.stateDB.UpdateRequest(record)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "blockchain tx failed"})
		return
	}

	record.Status = model.StatusPending
	record.TxHash = txHash
	record.Expiration = expiration
	if err := h.stateDB.UpdateRequest(record); err != nil {
		log.Printf("[HandleDeposit] UpdateRequest error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	log.Printf("[HandleDeposit] updated request id=%d status=PENDING txHash=%s", id, txHash)

	c.JSON(http.StatusOK, gin.H{
		"request_id": id,
		"user_id":    user.UserID,
		"tx_hash":    txHash,
		"status":     "PENDING",
	})
}

func (h *Handler) HandleRetryMint(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	record, err := h.stateDB.GetRequestByID(id)
	if err != nil {
		log.Printf("[HandleRetryMint] GetRequestByID error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if record == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "request not found"})
		return
	}
	if record.Type != model.TypeMint {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request is not a MINT type"})
		return
	}
	if record.Status != model.StatusFailure {
		c.JSON(http.StatusConflict, gin.H{"error": "retry is only allowed for FAILURE status"})
		return
	}

	user, err := h.stateDB.GetUserByID(record.UserID)
	if err != nil {
		log.Printf("[HandleRetryMint] GetUserByID error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if user == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user not found"})
		return
	}

	expiration := time.Now().Add(model.MintExpiration).Unix()
	txHash, err := h.blockchain.SendMintTx(context.Background(), user.UserID, user.Address, record.BankTx, record.Amount, expiration)
	if err != nil {
		log.Printf("[HandleRetryMint] SendMintTx error: %v", err)
		record.Status = model.StatusFailure
		record.ErrorCode = model.ErrorCodeTxFailed
		_ = h.stateDB.UpdateRequest(record)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "blockchain tx failed"})
		return
	}

	record.Status = model.StatusPending
	record.TxHash = txHash
	record.Expiration = expiration
	record.ErrorCode = model.ErrorCodeNone
	if err := h.stateDB.UpdateRequest(record); err != nil {
		log.Printf("[HandleRetryMint] UpdateRequest error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	log.Printf("[HandleRetryMint] retried request id=%d status=PENDING txHash=%s", id, txHash)

	c.JSON(http.StatusOK, gin.H{
		"request_id": id,
		"tx_hash":    txHash,
		"status":     "PENDING",
	})
}

type WithdrawalRequest struct {
	UserID          string `json:"user_id"          binding:"required"`
	Amount          int64  `json:"amount"           binding:"required"`
	PermitDeadline  int64  `json:"permit_deadline"  binding:"required"`
	PermitSignature string `json:"permit_signature" binding:"required"` // 0x 포함 hex 문자열
}

func (h *Handler) HandleWithdraw(c *gin.Context) {
	var req WithdrawalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount must be greater than 0"})
		return
	}

	// permit_signature: "0x..." hex → []byte
	sigHex := strings.TrimPrefix(req.PermitSignature, "0x")
	permitSig, err := hex.DecodeString(sigHex)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid permit_signature: " + err.Error()})
		return
	}

	user, err := h.stateDB.GetUserByID(req.UserID)
	if err != nil {
		log.Printf("[HandleWithdraw] GetUserByID error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if user == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user not found"})
		return
	}

	// burnForFiat 트랜잭션 전송
	expiration := time.Now().Add(model.BurnExpiration).Unix()
	txHash, err := h.blockchain.SendBurnTx(context.Background(), user.Address, req.Amount, expiration, req.PermitDeadline, permitSig)
	if err != nil {
		log.Printf("[HandleWithdraw] SendBurnTx error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "blockchain tx failed"})
		return
	}

	// tx 전송 성공 → REQUESTED 상태로 기록
	record := &model.Request{
		Type:       model.TypeBurn,
		Status:     model.StatusRequested,
		UserID:     user.UserID,
		TxHash:     txHash,
		Amount:     req.Amount,
		Expiration: expiration,
		Timestamp:  time.Now().UnixMilli(),
	}
	id, err := h.stateDB.InsertRequest(record)
	if err != nil {
		if errors.Is(err, db.ErrDuplicateTxHash) {
			log.Printf("[HandleWithdraw] duplicate txHash=%s", txHash)
			c.JSON(http.StatusConflict, gin.H{"error": "duplicate tx"})
			return
		}
		log.Printf("[HandleWithdraw] InsertRequest error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	log.Printf("[HandleWithdraw] inserted burn request id=%d user_id=%s txHash=%s status=REQUESTED", id, user.UserID, txHash)

	c.JSON(http.StatusOK, gin.H{
		"request_id": id,
		"user_id":    user.UserID,
		"tx_hash":    txHash,
		"status":     "REQUESTED",
	})
}

func (h *Handler) HandleGetRequest(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	record, err := h.stateDB.GetRequestByID(id)
	if err != nil {
		log.Printf("[HandleGetRequest] GetRequestByID error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if record == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "request not found"})
		return
	}

	c.JSON(http.StatusOK, record)
}
