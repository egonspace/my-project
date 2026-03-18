package db

import (
	"database/sql"
	"errors"
	"fmt"
	"gateway/model"

	"github.com/lib/pq"
)

const pgUniqueViolation = "23505"

var ErrDuplicateBankTx = errors.New("duplicate bank_tx")

const createUsersSQL = `
CREATE TABLE IF NOT EXISTS users (
    user_id    VARCHAR(255) PRIMARY KEY,
    address    VARCHAR(255) NOT NULL UNIQUE,
    account_no VARCHAR(255) NOT NULL
);
`

const createRequestsSQL = `
CREATE TABLE IF NOT EXISTS requests (
    id          SERIAL PRIMARY KEY,
    type        INTEGER      NOT NULL,
    status      INTEGER      NOT NULL,
    user_id     VARCHAR(255) NOT NULL REFERENCES users(user_id),
    bank_tx     VARCHAR(255),
    tx_hash     VARCHAR(255) NOT NULL DEFAULT '',
    timestamp   BIGINT       NOT NULL,
    expiration  BIGINT       NOT NULL DEFAULT 0,
    amount      BIGINT       NOT NULL,
    error_code  INTEGER      NOT NULL DEFAULT 0
);
`

// bank_tx가 NULL인 BURN 요청은 제외하고 MINT 요청에 대해서만 유일성 보장
const createIndexSQL = `
CREATE UNIQUE INDEX IF NOT EXISTS idx_requests_bank_tx
    ON requests (bank_tx)
    WHERE bank_tx IS NOT NULL;
`

type StateDB struct {
	db *sql.DB
}

func NewStateDB(dsn string) (*StateDB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping db: %w", err)
	}
	return &StateDB{db: db}, nil
}

func (s *StateDB) CreateTable() error {
	for _, stmt := range []string{createUsersSQL, createRequestsSQL, createIndexSQL} {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *StateDB) Close() error {
	return s.db.Close()
}

func (s *StateDB) InsertUser(user *model.User) error {
	query := `INSERT INTO users (user_id, address, account_no) VALUES ($1, $2, $3)`
	_, err := s.db.Exec(query, user.UserID, user.Address, user.AccountNo)
	if err != nil {
		return fmt.Errorf("InsertUser failed: %w", err)
	}
	return nil
}

func (s *StateDB) GetUserByID(userID string) (*model.User, error) {
	query := `SELECT user_id, address, account_no FROM users WHERE user_id = $1`
	user := &model.User{}
	err := s.db.QueryRow(query, userID).Scan(&user.UserID, &user.Address, &user.AccountNo)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetUserByID failed: %w", err)
	}
	return user, nil
}

func (s *StateDB) GetUserByAddress(address string) (*model.User, error) {
	query := `SELECT user_id, address, account_no FROM users WHERE address = $1`
	user := &model.User{}
	err := s.db.QueryRow(query, address).Scan(&user.UserID, &user.Address, &user.AccountNo)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetUserByAddress failed: %w", err)
	}
	return user, nil
}

func (s *StateDB) InsertRequest(req *model.Request) (int64, error) {
	// BURN 요청은 bank_tx가 없으므로 NULL로 저장
	var bankTx *string
	if req.BankTx != "" {
		bankTx = &req.BankTx
	}

	query := `
		INSERT INTO requests (type, status, user_id, bank_tx, tx_hash, timestamp, expiration, amount, error_code)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id
	`
	var id int64
	err := s.db.QueryRow(
		query,
		req.Type, req.Status, req.UserID, bankTx,
		req.TxHash, req.Timestamp, req.Expiration, req.Amount, req.ErrorCode,
	).Scan(&id)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == pgUniqueViolation {
			return 0, ErrDuplicateBankTx
		}
		return 0, fmt.Errorf("InsertRequest failed: %w", err)
	}
	return id, nil
}

func (s *StateDB) UpdateRequest(req *model.Request) error {
	query := `
		UPDATE requests
		SET status     = $1,
		    tx_hash    = $2,
		    expiration = $3,
		    error_code = $4
		WHERE id = $5
	`
	result, err := s.db.Exec(query, req.Status, req.TxHash, req.Expiration, req.ErrorCode, req.ID)
	if err != nil {
		return fmt.Errorf("UpdateRequest failed: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("UpdateRequest RowsAffected failed: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("UpdateRequest: no rows affected for id=%d", req.ID)
	}
	return nil
}

func (s *StateDB) GetRequestByID(id int64) (*model.Request, error) {
	query := `
		SELECT id, type, status, user_id, COALESCE(bank_tx, ''), tx_hash, timestamp, expiration, amount, error_code
		FROM requests
		WHERE id = $1
	`
	req := &model.Request{}
	err := s.db.QueryRow(query, id).Scan(
		&req.ID, &req.Type, &req.Status, &req.UserID,
		&req.BankTx, &req.TxHash, &req.Timestamp, &req.Expiration, &req.Amount, &req.ErrorCode,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetRequestByID failed: %w", err)
	}
	return req, nil
}

func (s *StateDB) GetRequestByBankTx(bankTx string) (*model.Request, error) {
	query := `
		SELECT id, type, status, user_id, COALESCE(bank_tx, ''), tx_hash, timestamp, expiration, amount, error_code
		FROM requests
		WHERE bank_tx = $1
	`
	req := &model.Request{}
	err := s.db.QueryRow(query, bankTx).Scan(
		&req.ID, &req.Type, &req.Status, &req.UserID,
		&req.BankTx, &req.TxHash, &req.Timestamp, &req.Expiration, &req.Amount, &req.ErrorCode,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetRequestByBankTx failed: %w", err)
	}
	return req, nil
}

func (s *StateDB) GetRequestByTxHash(txHash string) (*model.Request, error) {
	query := `
		SELECT id, type, status, user_id, COALESCE(bank_tx, ''), tx_hash, timestamp, expiration, amount, error_code
		FROM requests
		WHERE tx_hash = $1
	`
	req := &model.Request{}
	err := s.db.QueryRow(query, txHash).Scan(
		&req.ID, &req.Type, &req.Status, &req.UserID,
		&req.BankTx, &req.TxHash, &req.Timestamp, &req.Expiration, &req.Amount, &req.ErrorCode,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetRequestByTxHash failed: %w", err)
	}
	return req, nil
}

// GetStaleMintRequests returns MINT requests that are still REQUESTED or PENDING
// but whose on-chain expiration has already passed.
func (s *StateDB) GetStaleMintRequests(nowUnix int64) ([]*model.Request, error) {
	query := `
		SELECT id, type, status, user_id, COALESCE(bank_tx, ''), tx_hash, timestamp, expiration, amount, error_code
		FROM requests
		WHERE type       = $1
		  AND status     = $2
		  AND expiration > 0
		  AND expiration < $3
	`
	rows, err := s.db.Query(query, model.TypeMint, model.StatusPending, nowUnix)
	if err != nil {
		return nil, fmt.Errorf("GetStaleMintRequests failed: %w", err)
	}
	defer rows.Close()

	var results []*model.Request
	for rows.Next() {
		req := &model.Request{}
		if err := rows.Scan(
			&req.ID, &req.Type, &req.Status, &req.UserID,
			&req.BankTx, &req.TxHash, &req.Timestamp, &req.Expiration, &req.Amount, &req.ErrorCode,
		); err != nil {
			return nil, fmt.Errorf("GetStaleMintRequests scan failed: %w", err)
		}
		results = append(results, req)
	}
	return results, rows.Err()
}
