package rpc

type CreateUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type DeleteUserRequest struct {
	Email string `json:"email"`
}

type AuthenticateRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}
