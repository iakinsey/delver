package types

import "time"

type TAuthKey string
type TUserKey string

var AuthHeader = TAuthKey("auth")
var UserHeader = TUserKey("user")

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
