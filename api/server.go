package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/types/errs"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var corsHeaders = map[string]string{
	"Access-Control-Allow-Origin":  "*",
	"Access-Control-Allow-Headers": "*",
	"Access-Control-Allow-Methods": "GET, POST, OPTIONS",
	"Access-Control-Max-Age":       "1728000",
}

type Controller func(context.Context, json.RawMessage) (interface{}, error)
type APIResponse struct {
	Code    int         `json:"http_code,omitempty"`
	Success bool        `json:"success,omitempty"`
	Error   string      `json:"error,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func StartHTTPServer() {
	server, cleanup, err := NewServer(config.Get().API)
	if err != nil {
		log.Error(err)
		return
	}
	defer cleanup()
	if server == nil {
		return
	}
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Error(err)
	}
}

type Operations interface {
	Handle(context.Context, string, json.RawMessage) (interface{}, error)
}

func NewServer(conf config.APIConfig) (*http.Server, func(), error) {
	return NewServerWithOperations(conf, nil)
}
func NewServerWithOperations(conf config.APIConfig, operations Operations) (*http.Server, func(), error) {
	if !conf.Enabled {
		return nil, func() {}, nil
	}
	routes := map[string]Controller{}
	if workspace, ok := operations.(interface {
		HandleWorkspace(context.Context, string, json.RawMessage) (interface{}, error)
	}); ok {
		routes["/workspace/settings"] = func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			return workspace.HandleWorkspace(ctx, "settings", raw)
		}
	}

	if operations != nil {
		if multi, ok := operations.(interface {
			HandleCrawls(context.Context, string, json.RawMessage) (interface{}, error)
		}); ok {
			for _, name := range []string{"defaults", "patch_fetch", "preview", "results", "list", "create", "detail", "validate", "draft", "activate", "start_run", "continue_run", "duplicate", "delete", "archive", "restore", "command", "status", "operations", "failed", "log", "pipeline"} {
				action := name
				routes["/crawls/"+action] = func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
					return multi.HandleCrawls(ctx, action, raw)
				}
			}
		}

	}
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/crawler/") || strings.HasPrefix(r.URL.Path, "/dashboard/") {
			http.NotFound(w, r)
			return
		}
		if conf.StaticFiles != nil && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			if _, known := routes[r.URL.Path]; !known {
				ServeUI(conf.StaticFiles, w, r)
				return
			}
		}
		h := requestHandler{conf: conf, routes: routes, req: r, resp: w}
		h.Perform()
	})
	cleanup := func() {}

	return &http.Server{Addr: conf.Address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}, cleanup, nil
}

type requestHandler struct {
	conf   config.APIConfig
	routes map[string]Controller
	req    *http.Request
	resp   http.ResponseWriter
}

func (s *requestHandler) Perform() {
	resp, err := s.handle()

	if err != nil {
		s.respondError(err)
		return
	}

	s.respondSuccess(resp)
}

func (s *requestHandler) handle() (interface{}, error) {
	log.Infof("incoming api request for path %s", s.req.URL.String())

	if s.req.Method == http.MethodOptions && s.conf.AllowCors {
		return nil, nil
	}

	ctx := s.req.Context()

	if (strings.HasPrefix(s.req.URL.Path, "/crawls/") || strings.HasPrefix(s.req.URL.Path, "/workspace/")) && s.req.Method != http.MethodPost {
		return nil, errs.NewRequestError("POST required")
	}
	c, ok := s.routes[s.req.URL.Path]

	if !ok {
		return nil, errs.NewRequestError("Not found")
	}

	b, err := io.ReadAll(http.MaxBytesReader(s.resp, s.req.Body, 1<<20))

	if err != nil {
		log.Errorf("failed to read request body: %s", err)
		return nil, errs.NewRequestError("Malformed request")
	}

	var req json.RawMessage

	if len(b) > 0 {
		if err := json.Unmarshal(b, &req); err != nil {
			log.Errorf("failed to parse request body: %s", err)
			return nil, errs.NewRequestError("Malformed request body")
		}
	}

	return c(ctx, req)
}

func (s *requestHandler) respondSuccess(resp interface{}) {
	apiResp := APIResponse{
		Code:    http.StatusOK,
		Success: true,
		Data:    resp,
	}

	s.respond(http.StatusOK, apiResp)
}

func (s *requestHandler) respondError(theErr error) {
	httpCode := http.StatusInternalServerError
	apiResp := APIResponse{
		Success: false,
		Error:   theErr.Error(),
		Code:    errs.InternalError,
	}

	if userErr, ok := theErr.(*errs.ApplicationError); ok {
		httpCode = http.StatusBadRequest
		apiResp.Code = userErr.Code
	} else {
		log.Error(errors.Wrap(theErr, "handle request failure"))
	}

	s.respond(httpCode, apiResp)
}

func (s *requestHandler) respond(httpCode int, apiResp APIResponse) {
	s.resp.Header().Set("Content-Type", "application/json")

	if s.conf.AllowCors {
		for k, v := range corsHeaders {
			s.resp.Header().Set(k, v)
		}
	}

	s.resp.WriteHeader(httpCode)
	b, err := json.Marshal(apiResp)

	if err != nil {
		log.Error(errors.Wrap(err, "serialize response error"))
		return
	}

	if _, err = s.resp.Write(b); err != nil {
		log.Error(errors.Wrap(err, "write response error"))
		return
	}
}
