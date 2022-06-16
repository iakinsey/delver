package controller

import (
	"context"
	"encoding/json"

	"github.com/iakinsey/delver/gateway"
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
	return nil, nil
}
func (s *authController) DeleteUser(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	return nil, nil
}
func (s *authController) Authenticate(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	return nil, nil
}
func (s *authController) ChangePassword(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	return nil, nil
}
func (s *authController) Logout(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	return nil, nil
}
