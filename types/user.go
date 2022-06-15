package types

import "time"

type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	PasswordHash []byte `json:"password_hash"`
}

type Token struct {
	UserID  string    `json:"user_id"`
	Expires time.Time `json:"expires"`
	Value   string    `json:"value"`
}
