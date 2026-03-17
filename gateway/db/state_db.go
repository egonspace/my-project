package db

import (
	"database/sql"
	"fmt"
	"gateway/model"

	_ "github.com/lib/pq"
)

const createTableSQL = `
CREATE TABLE IF NOT EXISTS requests (
    id           SERIAL PRIMARY KEY,
    type         INTEGER      NOT NULL,
    status       INTEGER      NOT NULL,
    requester_id VARCHAR(255) NOT NULL,
    bank_tx      VARCHAR(255) NOT NULL DEFAULT '',
    tx_hash      VARCHAR(255) NOT NULL DEFAULT '',
    timestamp    BIGINT       NOT NULL,
    amount       BIGINT       NOT NULL,
    error_code   INTEGER      NOT NULL DEFAULT 0,
    UNIQUE (bank_tx)
);
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
	_, err := s.db.Exec(createTableSQL)
	return err
}

func (s *StateDB) Close() error {
	return s.db.Close()
}

func (s *StateDB) InsertRequest(req *model.Request) (int64, error) {
	query := `
		INSERT INTO requests (type, status, requester_id, bank_tx, tx_hash, timestamp, amount, error_code)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`
	var id int64
	err := s.db.QueryRow(
		query,
		req.Type,
		req.Status,
		req.RequesterID,
		req.BankTx,
		req.TxHash,
		req.Timestamp,
		req.Amount,
		req.ErrorCode,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("InsertRequest failed: %w", err)
	}
	return id, nil
}

func (s *StateDB) UpdateRequest(req *model.Request) error {
	query := `
		UPDATE requests
		SET status     = $1,
		    tx_hash    = $2,
		    error_code = $3
		WHERE id = $4
	`
	result, err := s.db.Exec(query, req.Status, req.TxHash, req.ErrorCode, req.ID)
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
		SELECT id, type, status, requester_id, bank_tx, tx_hash, timestamp, amount, error_code
		FROM requests
		WHERE id = $1
	`
	req := &model.Request{}
	err := s.db.QueryRow(query, id).Scan(
		&req.ID,
		&req.Type,
		&req.Status,
		&req.RequesterID,
		&req.BankTx,
		&req.TxHash,
		&req.Timestamp,
		&req.Amount,
		&req.ErrorCode,
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
		SELECT id, type, status, requester_id, bank_tx, tx_hash, timestamp, amount, error_code
		FROM requests
		WHERE bank_tx = $1
	`
	req := &model.Request{}
	err := s.db.QueryRow(query, bankTx).Scan(
		&req.ID,
		&req.Type,
		&req.Status,
		&req.RequesterID,
		&req.BankTx,
		&req.TxHash,
		&req.Timestamp,
		&req.Amount,
		&req.ErrorCode,
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
		SELECT id, type, status, requester_id, bank_tx, tx_hash, timestamp, amount, error_code
		FROM requests
		WHERE tx_hash = $1
	`
	req := &model.Request{}
	err := s.db.QueryRow(query, txHash).Scan(
		&req.ID,
		&req.Type,
		&req.Status,
		&req.RequesterID,
		&req.BankTx,
		&req.TxHash,
		&req.Timestamp,
		&req.Amount,
		&req.ErrorCode,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetRequestByTxHash failed: %w", err)
	}
	return req, nil
}
