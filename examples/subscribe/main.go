package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/websocket"
)

func main() {
	address := flag.String("url", "ws://localhost:26548", "subscription server")
	filters := flag.String("filter", `[{"data_type":"composite","query":{},"callback":"manual","options":{"preload":true}}]`, "JSON filter array")
	count := flag.Int("count", 1, "messages to receive; zero waits until interrupted")
	timeout := flag.Duration("timeout", 10*time.Second, "timeout per received message")
	flag.Parse()
	if err := run(*address, *filters, *count, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(address, filters string, count int, timeout time.Duration) error {
	var validated []json.RawMessage
	if err := json.Unmarshal([]byte(filters), &validated); err != nil {
		return err
	}
	if len(validated) == 0 {
		return fmt.Errorf("at least one filter is required")
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return err
	}
	origin := "http://" + parsed.Host
	if parsed.Scheme == "wss" {
		origin = "https://" + parsed.Host
	}
	conn, err := websocket.Dial(strings.TrimSuffix(address, "/")+"/", "", origin)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := websocket.JSON.Send(conn, map[string]interface{}{"version": 1, "type": "authenticate", "token": os.Getenv("DELVER_AUTH_TOKEN")}); err != nil {
		return err
	}
	var ack map[string]interface{}
	if err := websocket.JSON.Receive(conn, &ack); err != nil {
		return err
	}
	if ack["type"] != "authenticated" {
		return fmt.Errorf("authentication failed")
	}
	for i, fp := range validated {
		if err := websocket.JSON.Send(conn, map[string]interface{}{"version": 1, "type": "subscribe", "id": fmt.Sprint(i), "filter": fp}); err != nil {
			return err
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() { <-ctx.Done(); conn.Close() }()
	for received := 0; count == 0 || received < count; received++ {
		conn.SetReadDeadline(time.Now().Add(timeout))
		var message json.RawMessage
		if err := websocket.JSON.Receive(conn, &message); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		fmt.Println(string(message))
	}
	return nil
}
