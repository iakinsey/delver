package api

import (
	"context"
	"encoding/json"
	"github.com/iakinsey/delver/config"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type operationsProbe struct{ calls int }

func (p *operationsProbe) Handle(context.Context, string, json.RawMessage) (interface{}, error) {
	p.calls++
	return nil, nil
}
func (p *operationsProbe) HandleCrawls(context.Context, string, json.RawMessage) (interface{}, error) {
	p.calls++
	return map[string]string{"state": "running"}, nil
}
func (p *operationsProbe) HandleWorkspace(context.Context, string, json.RawMessage) (interface{}, error) {
	return map[string]int{"revision": 1}, nil
}
func TestRetiredDashboardAndSingleCrawlRoutesAreUnavailable(t *testing.T) {
	conf := config.LoadConfig().API
	conf.DashDBPath = filepath.Join(t.TempDir(), "dashboard.db")
	probe := &operationsProbe{}
	server, cleanup, err := NewServerWithOperations(conf, probe)
	require.NoError(t, err)
	defer cleanup()
	for _, path := range []string{"/dashboard/save", "/dashboard/load", "/dashboard/delete", "/dashboard/list", "/crawler/status", "/crawler/command", "/crawler/draft", "/crawler/operations"} {
		for _, method := range []string{"GET", "POST"} {
			res := httptest.NewRecorder()
			server.Handler.ServeHTTP(res, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
			require.Equal(t, http.StatusNotFound, res.Code, path)
		}
	}
	_, err = os.Stat(conf.DashDBPath)
	require.True(t, os.IsNotExist(err))
	require.Zero(t, probe.calls)
	for _, path := range []string{"/crawls/list", "/crawls/command", "/workspace/settings"} {
		res := httptest.NewRecorder()
		server.Handler.ServeHTTP(res, httptest.NewRequest("POST", path, strings.NewReader(`{}`)))
		var value APIResponse
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &value))
		require.True(t, value.Success, path)
	}
}
