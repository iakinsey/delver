package frontierworker

import (
	"context"
	"encoding/json"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
)

type Admission struct {
	Store               frontier.FrontierStore
	AllowedHostSuffixes []string
}

func (w *Admission) OnComplete() {}
func (w *Admission) OnMessage(m types.Message) (interface{}, error) {
	return w.OnMessageContext(context.Background(), m)
}
func (w *Admission) OnMessageContext(ctx context.Context, m types.Message) (interface{}, error) {
	var r message.FetcherRequest
	if err := json.Unmarshal(m.Message, &r); err != nil {
		return nil, err
	}
	if !util.URLHostnameAllowed(r.URI, w.AllowedHostSuffixes) {
		return nil, nil
	}
	if r.ScopeRoot == "" {
		r.ScopeRoot = r.URI
		r.Depth = 0
		r.Hops = 0
	}
	if err := w.Store.StageDiscovery(ctx, m.ID, []message.FetcherRequest{r}); err != nil {
		return nil, err
	}
	_, err := w.Store.DrainDiscovery(ctx, 128)
	return nil, err
}
