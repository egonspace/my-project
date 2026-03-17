package api

import (
	"context"
	"gateway/blockchain"
	"gateway/db"
	"gateway/model"
	"log"
	"net/http"
	"strconv"
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
	RequesterID string `json:"requester_id" binding:"required"`
	ToAddress   string `json:"to_address"   binding:"required"`
	BankTx      string `json:"bank_tx"      binding:"required"`
	Amount      int64  `json:"amount"       binding:"required"`
}

type RetryMintRequest struct {
	Expiration int64 `json:"expiration" binding:"required"`
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

	existing, err := h.stateDB.GetRequestByBankTx(req.BankTx)
	if err != nil {
		log.Printf("[HandleDeposit] GetRequestByBankTx error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if existing != nil {
		log.Printf("[HandleDeposit] duplicate bank_tx=%s, existing requestID=%d", req.BankTx, existing.ID)
		c.JSON(http.StatusConflict, gin.H{
			"error":      "duplicate bank_tx",
			"request_id": existing.ID,
		})
		return
	}

	record := &model.Request{
		Type:        model.TypeMint,
		Status:      model.StatusRequested,
		RequesterID: req.RequesterID,
		BankTx:      req.BankTx,
		Amount:      req.Amount,
		Timestamp:   time.Now().UnixMilli(),
	}
	id, err := h.stateDB.InsertRequest(record)
	if err != nil {
		log.Printf("[HandleDeposit] InsertRequest error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	record.ID = id
	log.Printf("[HandleDeposit] inserted request id=%d status=REQUESTED", id)

	expiration := time.Now().Add(10 * time.Minute).Unix()
	txHash, err := h.blockchain.SendMintTx(context.Background(), req.RequesterID, req.ToAddress, req.Amount, expiration)
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
	if err := h.stateDB.UpdateRequest(record); err != nil {
		log.Printf("[HandleDeposit] UpdateRequest error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	log.Printf("[HandleDeposit] updated request id=%d status=PENDING txHash=%s", id, txHash)

	c.JSON(http.StatusOK, gin.H{
		"request_id": id,
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

	var req RetryMintRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	if record.Status == model.StatusSuccess {
		c.JSON(http.StatusConflict, gin.H{"error": "request already succeeded"})
		return
	}
	if record.Status == model.StatusPending {
		c.JSON(http.StatusConflict, gin.H{"error": "request is already pending"})
		return
	}

	txHash, err := h.blockchain.SendMintTx(context.Background(), record.RequesterID, "", record.Amount, req.Expiration)
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
