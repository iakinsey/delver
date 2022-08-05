package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"

	"github.com/iakinsey/delver/api/controller"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/errs"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var corsHeaders = map[string]string{
	"Access-Control-Allow-Origin":      "*",
	"Access-Control-Allow-Headers":     "*",
	"Access-Control-Allow-Credentials": "true",
	"Access-Control-Allow-Methods":     "GET, POST, OPTIONS",
	"Access-Control-Max-Age":           "1728000",
}
var noAuthWhitelist = []string{
	"/user/create",
	"/user/authenticate",
}

type Controller func(context.Context, json.RawMessage) (interface{}, error)
type APIResponse struct {
	Code    int         `json:"http_code,omitempty"`
	Success bool        `json:"success,omitempty"`
	Error   string      `json:"error,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func StartHTTPServer() {
	conf := config.Get().API

	if !conf.Enabled {
		return
	}

	// TODO set user gateway path
	user := gateway.NewUserGateway(conf.UserDBPath)
	handler := http.NewServeMux()
	l, err := net.Listen("tcp", conf.Address)
	routes := getRoutes(conf, user)

	if err != nil {
		log.Fatalf("failed to start http server %s", err)
	}

	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h := requestHandler{
			user:   user,
			conf:   conf,
			routes: routes,
			req:    r,
			resp:   w,
		}

		h.Perform()
	})

	log.Infof("http server listening on %s", conf.Address)
	log.Fatal(http.Serve(l, handler))
}

func getRoutes(conf config.APIConfig, user gateway.UserGateway) map[string]Controller {
	routes := make(map[string]Controller)
	dash := controller.NewDashboardController(gateway.NewDashboardGateway(conf.DashDBPath))
	auth := controller.NewAuthController(user)

	routes["/dashboard/save"] = dash.Save
	routes["/dashboard/load"] = dash.Load
	routes["/dashboard/delete"] = dash.Delete
	routes["/dashboard/list"] = dash.List
	routes["/user/create"] = auth.CreateUser
	routes["/user/delete"] = auth.DeleteUser
	routes["/user/authenticate"] = auth.Authenticate
	routes["/user/change_password"] = auth.ChangePassword
	routes["/user/logout"] = auth.Logout

	return routes
}

type requestHandler struct {
	user   gateway.UserGateway
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

	ctx, err := s.getAuthContext()

	if err != nil {
		return nil, err
	}

	c, ok := s.routes[s.req.URL.Path]

	if !ok {
		return nil, errs.NewRequestError("Not found")
	}

	b, err := io.ReadAll(s.req.Body)

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

func (s *requestHandler) getAuthContext() (context.Context, error) {
	parent := s.req.Context()
	path := s.req.URL.Path
	auth := s.req.Header.Get(string(types.AuthHeader))

	// If the route doesn't require auth and no header is set then
	// return the parent context
	if auth == "" && util.StringInSlice(path, noAuthWhitelist) {
		return parent, nil
	}

	t, err := s.user.ValidateToken(auth)

	if err != nil {
		return nil, err
	}

	u, err := s.user.Get(t.UserID)

	if err != nil {
		return nil, err
	}

	return context.WithValue(
		context.WithValue(parent, types.AuthHeader, t),
		types.UserHeader,
		u,
	), nil
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
