package controller

import (
	"encoding/json"
	"os"
	"path"

	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/types/instrument"
)

type MetricsController interface {
	Put(msg json.RawMessage) (interface{}, error)
	Get(msg json.RawMessage) (interface{}, error)
	List(msg json.RawMessage) (interface{}, error)
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

func (s *metricsController) Put(msg json.RawMessage) (interface{}, error) {
	var request map[string][]instrument.Metric

	if err := json.Unmarshal(msg, &request); err != nil {
		return nil, err
	}

	return nil, s.driver.Put(request)
}

func (s *metricsController) Get(msg json.RawMessage) (interface{}, error) {
	var query instrument.MetricsQuery

	if err := json.Unmarshal(msg, &query); err != nil {
		return nil, err
	}

	return s.driver.Get(query)
}

func (s *metricsController) List(msg json.RawMessage) (interface{}, error) {
	return s.driver.List()
}
