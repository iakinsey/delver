package controller

import "encoding/json"

type MetricsController interface {
	Push(msg json.RawMessage) (interface{}, error)
	Get(msg json.RawMessage) (interface{}, error)
}

type metricsController struct {
}

func NewMetricsController() MetricsController {
	return &metricsController{}
}

func (s *metricsController) Push(msg json.RawMessage) (interface{}, error) {
	return "haha look at me im a push request", nil
}

func (s *metricsController) Get(msg json.RawMessage) (interface{}, error) {
	return "this is get", nil
}
