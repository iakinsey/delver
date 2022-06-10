package gateway

import "github.com/iakinsey/delver/types/instrument"

type MetricsGateway interface {
	Get(instrument.MetricsQuery) ([]instrument.Metric, error)
	Put(map[string][]instrument.Metric) error
	List() ([]string, error)
}
