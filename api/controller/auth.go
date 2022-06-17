package controller

import (
	"context"
	"encoding/json"

	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/types/errs"
	"github.com/iakinsey/delver/types/rpc"
)

type AuthController interface {
	CreateUser(context.Context, json.RawMessage) (interface{}, error)
	DeleteUser(context.Context, json.RawMessage) (interface{}, error)
	Authenticate(context.Context, json.RawMessage) (interface{}, error)
	ChangePassword(context.Context, json.RawMessage) (interface{}, error)
	Logout(context.Context, json.RawMessage) (interface{}, error)
}

type authController struct {
	auth gateway.UserGateway
}

func NewAuthController(g gateway.UserGateway) AuthController {
	return &authController{
		auth: g,
	}
}

func (s *authController) CreateUser(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	req := rpc.CreateUserRequest{}

	if err := json.Unmarshal(msg, &req); err != nil {
		return nil, err
	}

	_, err := s.auth.Create(req.Email, req.Password)

	if err != nil {
		return nil, err
	}

	return s.authenticate(req.Email, req.Password)
}

func (s *authController) DeleteUser(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	req := rpc.CreateUserRequest{}

	if err := json.Unmarshal(msg, &req); err != nil {
		return nil, err
	}

	if err := CurrentUserMatchesEmail(ctx, req.Email); err != nil {
		return nil, err
	}

	return nil, s.auth.Delete(req.Email)
}

func (s *authController) Authenticate(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	req := rpc.AuthenticateRequest{}

	if err := json.Unmarshal(msg, &req); err != nil {
		return nil, err
	}

	return s.authenticate(req.Email, req.Password)
}

func (s *authController) ChangePassword(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	req := rpc.ChangePasswordRequest{}

	if err := json.Unmarshal(msg, &req); err != nil {
		return nil, err
	}

	user := GetCurrentUser(ctx)

	if user == nil {
		return nil, errs.NewAuthError("Unauthorized")
	}

	if err := gateway.CheckPassword(req.OldPassword, user.PasswordHash); err != nil {
		return nil, err
	}

	token, err := s.auth.ChangePassword(user.ID, req.NewPassword)

	if err != nil {
		return nil, err
	}

	return token.Value, nil
}

func (s *authController) Logout(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	token := GetCurrentToken(ctx)

	return nil, s.auth.Deauthenticate(token.Value)
}

func (s *authController) authenticate(email, pass string) (interface{}, error) {
	token, err := s.auth.Authenticate(email, pass)

	if err != nil {
		return nil, err
	}

	return token.Value, nil
}
