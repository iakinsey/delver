package controller

import (
	"context"
	"encoding/json"

	"github.com/iakinsey/delver/gateway"
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
	return nil, nil
}

func (s *dashController) Load(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	return nil, nil
}

func (s *dashController) Delete(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	return nil, nil
}

func (s *dashController) List(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	return nil, nil
}
