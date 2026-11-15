package gateway

import (
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/rpc"
)

type MetricsGateway interface {
	Get(rpc.MetricFilterQuery) ([]types.Metric, error)
	Put(map[string][]types.Metric) error
	List() ([]string, error)
}
