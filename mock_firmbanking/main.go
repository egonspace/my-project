package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

type TransferRequest struct {
	ToAccountNo string `json:"to_account_no"`
	Amount      int64  `json:"amount"`
	RefID       string `json:"ref_id"`
}

type TransferResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

var (
	transferCount int
	logger        = log.New(os.Stdout, "", 0)
)

func logf(format string, args ...any) {
	logger.Printf("[%s] "+format, append([]any{time.Now().Format("15:04:05.000")}, args...)...)
}

func handleTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logf("❌ Transfer - invalid request body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TransferResponse{Success: false, Message: "invalid request"})
		return
	}

	transferCount++
	logf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logf("📨 Transfer 요청 #%d", transferCount)
	logf("   to_account_no : %s", req.ToAccountNo)
	logf("   amount        : %d 원", req.Amount)
	logf("   ref_id        : %s", req.RefID)

	// 유효성 검사
	if req.Amount <= 0 {
		logf("❌ Transfer 실패 - 금액 오류: %d", req.Amount)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TransferResponse{Success: false, Message: fmt.Sprintf("invalid amount: %d", req.Amount)})
		return
	}
	if req.ToAccountNo == "" {
		logf("❌ Transfer 실패 - 계좌번호 없음")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TransferResponse{Success: false, Message: "to_account_no is empty"})
		return
	}

	logf("✅ Transfer 성공")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TransferResponse{Success: true, Message: "ok"})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":          "ok",
		"transfer_count":  transferCount,
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/transfer", handleTransfer)
	mux.HandleFunc("/health", handleHealth)

	logf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logf("🏦 Mock FirmBanking 서버 시작")
	logf("   POST http://localhost:%s/transfer", port)
	logf("   GET  http://localhost:%s/health", port)
	logf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("서버 시작 실패: %v", err)
	}
}
