package controller

import (
	"context"
	"encoding/json"

	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/errs"
	"github.com/iakinsey/delver/types/rpc"
)

type DashController interface {
	Save(context.Context, json.RawMessage) (interface{}, error)
	Load(context.Context, json.RawMessage) (interface{}, error)
	Delete(context.Context, json.RawMessage) (interface{}, error)
	List(context.Context, json.RawMessage) (interface{}, error)
}

type dashController struct {
	gateway gateway.DashboardGateway
}

func NewDashboardController(g gateway.DashboardGateway) DashController {
	return &dashController{
		gateway: g,
	}
}

func (s *dashController) Save(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	req := rpc.SaveDashboardRequest{}

	if err := json.Unmarshal(msg, &req); err != nil {
		return nil, err
	}

	if req.ID == "" {
		req.ID = string(types.NewV4())
	}

	user := GetCurrentUser(ctx)

	if user == nil {
		return nil, errs.NewAuthError("Unauthorized")
	}

	return s.gateway.Put(types.Dashboard{
		ID:          req.ID,
		Name:        req.Name,
		UserID:      user.ID,
		Value:       req.Value,
		Description: req.Description,
	}), nil
}

func (s *dashController) Load(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	req := rpc.LoadDashboardRequest{}

	if err := json.Unmarshal(msg, &req); err != nil {
		return nil, err
	}

	return s.getDash(ctx, req.ID)
}

func (s *dashController) Delete(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	req := rpc.DeleteDashboardRequest{}

	if err := json.Unmarshal(msg, &req); err != nil {
		return nil, err
	}

	d, err := s.getDash(ctx, req.ID)

	if err != nil {
		return nil, err
	}

	return nil, s.gateway.Delete(d.ID)
}

func (s *dashController) List(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	user := GetCurrentUser(ctx)

	if user == nil {
		return nil, errs.NewAuthError("unauthorized")
	}

	return s.gateway.List(user.ID)
}

func (s *dashController) getDash(ctx context.Context, id string) (*types.Dashboard, error) {
	user := GetCurrentUser(ctx)

	if user == nil {
		return nil, errs.NewAuthError("unauthorized")
	}

	d, err := s.gateway.Get(id)

	if err != nil {
		return nil, err
	}

	if d.UserID != user.ID {
		return nil, errs.NewAuthError("Unauthorized")
	}

	return d, err
}
