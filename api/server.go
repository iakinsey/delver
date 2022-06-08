package api

import (
	"encoding/json"
	"io"
	"net"
	"net/http"

	"github.com/iakinsey/delver/api/controller"
	"github.com/iakinsey/delver/config"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type Controller func(json.RawMessage) (interface{}, error)
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

	handler := http.NewServeMux()
	l, err := net.Listen("tcp", conf.Address)
	routes := getRoutes()

	if err != nil {
		log.Fatalf("failed to start http server %s", err)
	}

	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleRequest(routes, w, r); err != nil {
			log.Errorf("handle request failure: %s", err)
		}
	})

	log.Infof("http server listening on %s", conf.Address)
	log.Fatal(http.Serve(l, handler))
}

func getRoutes() map[string]Controller {
	routes := make(map[string]Controller)
	metrics := controller.NewMetricsController()

	routes["/metrics/put"] = metrics.Put
	routes["/metrics/get"] = metrics.Get

	return routes
}

func handleRequest(t map[string]Controller, w http.ResponseWriter, r *http.Request) error {
	log.Info("incoming api request for path %s", r.URL.Path)

	c, ok := t[r.URL.Path]

	if !ok {
		return respondError(http.StatusNotFound, "", w)
	}

	b, err := io.ReadAll(r.Body)

	if err != nil {
		log.Errorf("failed to read request body: %s", err)
		return respondError(http.StatusBadRequest, "", w)
	}

	var req json.RawMessage

	if err := json.Unmarshal(b, &req); err != nil {
		log.Errorf("failed to parse request body: %s", err)
		return respondError(http.StatusBadRequest, "", w)
	}

	resp, err := c(req)

	if err != nil {
		log.Errorf("failure when calling controller: %s", err)
		return respondError(http.StatusInternalServerError, "", w)
	}

	return respondSuccess(resp, w)
}

func respondSuccess(resp interface{}, w http.ResponseWriter) error {
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	apiResp := APIResponse{
		Code:    http.StatusOK,
		Success: true,
		Data:    resp,
	}

	return respond(apiResp, w)
}

func respondError(code int, msg string, w http.ResponseWriter) error {
	w.WriteHeader(code)
	w.Header().Set("Content-Type", "application/json")

	if msg == "" {
		msg = http.StatusText(code)
	}

	apiResp := APIResponse{
		Code:    code,
		Success: false,
		Error:   msg,
	}

	return respond(apiResp, w)
}

func respond(apiResp APIResponse, w http.ResponseWriter) error {
	b, err := json.Marshal(apiResp)

	if err != nil {
		return errors.Wrap(err, "serialize response error")
	}

	if _, err = w.Write(b); err != nil {
		return errors.Wrap(err, "write response error")
	}

	return nil
}
