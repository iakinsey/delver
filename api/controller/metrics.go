package controller

import (
	"context"
	"encoding/json"
	"os"
	"path"

	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/types/instrument"
)

type MetricsController interface {
	Put(context.Context, json.RawMessage) (interface{}, error)
	Get(context.Context, json.RawMessage) (interface{}, error)
	List(context.Context, json.RawMessage) (interface{}, error)
}

type metricsController struct {
	driver gateway.MetricsGateway
}

func NewMetricsController() MetricsController {
	driver := gateway.NewMetricSqlite(path.Join(os.TempDir(), "metrics.db"))

	return &metricsController{
		driver: driver,
	}
}

func (s *metricsController) Put(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	var request map[string][]instrument.Metric

	if err := json.Unmarshal(msg, &request); err != nil {
		return nil, err
	}

	return nil, s.driver.Put(request)
}

func (s *metricsController) Get(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	var query instrument.MetricsQuery

	if err := json.Unmarshal(msg, &query); err != nil {
		return nil, err
	}

	return s.driver.Get(query)
}

func (s *metricsController) List(ctx context.Context, msg json.RawMessage) (interface{}, error) {
	return s.driver.List()
}
