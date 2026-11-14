package gateway

import (
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/filter"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/rpc"
	"golang.org/x/net/websocket"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type sessionCommand struct {
	Version int             `json:"version"`
	Type    string          `json:"type"`
	Token   string          `json:"token"`
	ID      string          `json:"id"`
	Filter  json.RawMessage `json:"filter"`
}

func (s *clientStreamer) serveSession(conn *websocket.Conn, auth UserGateway) {
	defer conn.Close()
	conn.MaxPayloadBytes = 64 << 10
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var first sessionCommand
	if websocket.JSON.Receive(conn, &first) != nil || first.Version != 1 || first.Type != "authenticate" {
		return
	}
	validate := func() bool {
		t, err := auth.ValidateToken(first.Token)
		if err != nil {
			return false
		}
		_, err = auth.Get(t.UserID)
		return err == nil
	}
	if !validate() {
		websocket.JSON.Send(conn, map[string]string{"type": "auth_error"})
		return
	}
	conn.SetReadDeadline(time.Time{})
	sender := newClientSender(conn)
	defer sender.stop()
	control := func(kind, id, message string) {
		data, _ := json.Marshal([]map[string]string{{"callback": id, "message": message}})
		sender.enqueue(types.ClientStreamerMessage{Type: kind, Data: data})
	}
	control("authenticated", "", "")
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if !validate() {
					control("auth_error", "", "Session expired")
					sender.stop()
					return
				}
			}
		}
	}()
	owned := map[string]string{}
	defer func() {
		for _, key := range owned {
			s.removeSessionClient(key)
		}
	}()
	for {
		var cmd sessionCommand
		if websocket.JSON.Receive(conn, &cmd) != nil {
			return
		}
		if !validate() {
			control("auth_error", "", "Session expired")
			return
		}
		if cmd.Version != 1 || cmd.ID == "" {
			control("error", cmd.ID, "Invalid command")
			continue
		}
		if cmd.Type == "remove" {
			if key, ok := owned[cmd.ID]; ok {
				s.removeSessionClient(key)
				delete(owned, cmd.ID)
			}
			control("removed", cmd.ID, "")
			continue
		}
		if cmd.Type != "subscribe" {
			control("error", cmd.ID, "Unknown command")
			continue
		}
		if _, exists := owned[cmd.ID]; !exists && len(owned) >= 100 {
			control("error", cmd.ID, "Subscription limit reached")
			continue
		}
		raw, err := s.decodeFilters(append(append([]byte{'['}, cmd.Filter...), ']'))
		if err != nil {
			control("error", cmd.ID, err.Error())
			continue
		}
		var fp rpc.FilterParams
		for _, v := range raw {
			fp = v
		}
		if fp.Agg != nil && (fp.Agg.TimeWindowSeconds <= 0 || (fp.Agg.Name != "sum" && fp.Agg.Name != "avg")) {
			control("error", cmd.ID, "Invalid aggregation")
			continue
		}
		if fp.DataType == rpc.FilterTypeMetric {
			q := fp.Query.(rpc.MetricFilterQuery)
			if q.Key == "" || (q.Start != 0 && q.End != 0 && q.Start > q.End) {
				control("error", cmd.ID, "Metric key and chronological dates are required")
				continue
			}
		}
		if fp.Agg != nil && (fp.Agg.TimeField == "" || fp.Agg.AggField == "") {
			control("error", cmd.ID, "Aggregation fields are required")
			continue
		}
		if key, ok := owned[cmd.ID]; ok {
			s.removeSessionClient(key)
		}
		fp.Callback = cmd.ID
		key := fmt.Sprintf("%p/%s", conn, cmd.ID)
		fp.AggregationID = key
		c := client{sender: sender, conn: conn, filter: fp, streamFilter: filter.GetStreamFilter(fp), delivery: &subscriptionDelivery{mutex: sync.Mutex{}}}
		owned[cmd.ID] = key
		// Register while delivery is locked: publications arriving during preload wait
		// and follow the historical batch without racing aggregation state.
		c.delivery.mutex.Lock()
		s.mutex.Lock()
		s.clients[key] = c
		s.mutex.Unlock()
		control("subscribed", cmd.ID, "")
		if fp.Options["preload"] {
			if err := s.Preload(c); err != nil {
				control("error", cmd.ID, "Unable to load history")
				c.delivery.mutex.Unlock()
				s.removeSessionClient(key)
				delete(owned, cmd.ID)
				continue
			}
		}
		control("preload_complete", cmd.ID, "")
		c.delivery.mutex.Unlock()
	}
}
func (s *clientStreamer) removeSessionClient(key string) {
	s.mutex.Lock()
	c, ok := s.clients[key]
	delete(s.clients, key)
	s.mutex.Unlock()
	if ok {
		if c.delivery != nil {
			c.delivery.mutex.Lock()
			defer c.delivery.mutex.Unlock()
			c.delivery.closed = true
		}
		s.flushClient(c)
	}
}

func validateStreamOrigin(r *http.Request, allowed []string) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return fmt.Errorf("origin required")
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return err
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid origin")
	}
	for _, candidate := range allowed {
		if origin == candidate {
			return nil
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if len(allowed) == 0 && parsed.Host == r.Host && parsed.Scheme == scheme {
		return nil
	}
	return fmt.Errorf("origin denied")
}
