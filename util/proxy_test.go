package util

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/iakinsey/delver/config"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAuthenticatedSOCKSProxy(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "through SOCKS") }))
	defer origin.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	conns := map[net.Conn]bool{}
	auth := make(chan string, 1)
	errs := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns[conn] = true
			mu.Unlock()
			wg.Add(1)
			go func(conn net.Conn) {
				defer wg.Done()
				defer conn.Close()
				report := func(err error) {
					select {
					case errs <- err:
					default:
					}
				}
				head := make([]byte, 2)
				if _, err := io.ReadFull(conn, head); err != nil {
					report(err)
					return
				}
				methods := make([]byte, int(head[1]))
				io.ReadFull(conn, methods)
				conn.Write([]byte{5, 2})
				if _, err := io.ReadFull(conn, head); err != nil {
					report(err)
					return
				}
				user := make([]byte, int(head[1]))
				io.ReadFull(conn, user)
				size := []byte{0}
				io.ReadFull(conn, size)
				password := make([]byte, int(size[0]))
				io.ReadFull(conn, password)
				auth <- string(user) + ":" + string(password)
				conn.Write([]byte{1, 0})
				request := make([]byte, 4)
				if _, err := io.ReadFull(conn, request); err != nil {
					report(err)
					return
				}
				addressBytes := 0
				switch request[3] {
				case 1:
					addressBytes = 4
				case 4:
					addressBytes = 16
				case 3:
					io.ReadFull(conn, size)
					addressBytes = int(size[0])
				default:
					report(fmt.Errorf("invalid SOCKS address type"))
					return
				}
				address := make([]byte, addressBytes+2)
				io.ReadFull(conn, address)
				if binary.BigEndian.Uint16(address[len(address)-2:]) == 0 {
					report(fmt.Errorf("missing destination port"))
					return
				}
				target, err := net.Dial("tcp", strings.TrimPrefix(origin.URL, "http://"))
				if err != nil {
					report(err)
					return
				}
				defer target.Close()
				conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				copyDone := make(chan struct{})
				go func() { io.Copy(target, conn); target.Close(); close(copyDone) }()
				io.Copy(conn, target)
				conn.Close()
				<-copyDone
			}(conn)
		}
	}()
	defer func() {
		listener.Close()
		mu.Lock()
		for conn := range conns {
			conn.Close()
		}
		mu.Unlock()
		wg.Wait()
	}()
	c := config.LoadConfig().HTTPClient
	c.Socks5Url = "socks5://operator:secret-password@" + listener.Addr().String()
	client := NewHTTPClientWithConfig(c)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	response, err := client.(interface {
		PerformContext(context.Context, string) (*http.Response, error)
	}).PerformContext(ctx, "http://proxy-dns.test:8081/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != "through SOCKS" {
		t.Fatal(string(body), err)
	}
	select {
	case value := <-auth:
		if value != "operator:secret-password" {
			t.Fatal("proxy authentication mismatch")
		}
	case err := <-errs:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
func TestSOCKSProxyValidation(t *testing.T) {
	for _, raw := range []string{"127.0.0.1:1080", "socks5://127.0.0.1:1080", "socks5://user:pass@127.0.0.1:1080"} {
		if _, _, err := ParseSOCKSProxy(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{"missing-port", "http://proxy:1080", "socks5://proxy:1080/path"} {
		if _, _, err := ParseSOCKSProxy(raw); err == nil {
			t.Fatal("accepted invalid proxy", raw)
		}
	}
}
