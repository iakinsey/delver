package types

type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	PasswordHash []byte `json:"password_hash"`
}
