package model

type User struct {
	UserID    string `json:"user_id"    db:"user_id"`
	Address   string `json:"address"    db:"address"`
	AccountNo string `json:"account_no" db:"account_no"`
}
