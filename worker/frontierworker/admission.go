package frontierworker

import (
	"context"
	"encoding/json"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/report"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
)

type Admission struct {
	Reporter            report.Writer
	Store               frontier.FrontierStore
	AllowedHostSuffixes []string
}

func (w *Admission) SetReporter(r report.Writer) { w.Reporter = r }
func (w *Admission) OnComplete()                 {}
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
	if w.Reporter != nil && !util.URLFileExcluded(r.URI, admissionExtensions(w.Store)) {
		if err := w.Reporter.Discover(ctx, []report.URL{{URI: r.URI, Origin: r.Origin}}); err != nil {
			return nil, err
		}
	}
	if err := w.Store.StageDiscovery(ctx, m.ID, []message.FetcherRequest{r}); err != nil {
		return nil, err
	}
	_, err := w.Store.DrainDiscovery(ctx, 128)
	return nil, err
}

func admissionExtensions(s frontier.FrontierStore) []string {
	if x, ok := s.(interface{ ExcludedFileExtensions() []string }); ok {
		return x.ExcludedFileExtensions()
	}
	return nil
}
