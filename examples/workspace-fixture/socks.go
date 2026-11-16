package main

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var socksEvents struct {
	sync.Mutex
	Rows []socksEvent
}

type socksEvent struct {
	User        string `json:"user"`
	Host        string `json:"host"`
	AddressType byte   `json:"address_type"`
}

// This local-only authenticated SOCKS5 fixture lets browser tests prove that
// feeds, robots, redirects and page requests use the crawl's selected proxy.
func startSOCKSFixture(ctx context.Context) (net.Listener, error) {
	listener, err := net.Listen("tcp", fixtureSOCKSAddress())
	if err != nil {
		return nil, err
	}
	go func() { <-ctx.Done(); listener.Close() }()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveSOCKSFixture(ctx, conn)
		}
	}()
	return listener, nil
}
func serveSOCKSFixture(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	read := func(n int) ([]byte, error) { b := make([]byte, n); _, err := io.ReadFull(conn, b); return b, err }
	readString := func() (string, error) {
		b, err := read(1)
		if err != nil {
			return "", err
		}
		v, err := read(int(b[0]))
		return string(v), err
	}
	hello, err := read(2)
	if err != nil || hello[0] != 5 {
		return
	}
	methods, err := read(int(hello[1]))
	if err != nil {
		return
	}
	auth := false
	for _, m := range methods {
		if m == 2 {
			auth = true
		}
	}
	if !auth {
		conn.Write([]byte{5, 255})
		return
	}
	conn.Write([]byte{5, 2})
	version, err := read(1)
	if err != nil || version[0] != 1 {
		return
	}
	user, err := readString()
	if err != nil {
		return
	}
	password, err := readString()
	if err != nil {
		return
	}
	if !(user == "operator" && password == "browser-proxy-secret") && !(strings.HasPrefix(user, "session-") && len(password) == 64) {
		conn.Write([]byte{1, 1})
		return
	}
	conn.Write([]byte{1, 0})
	request, err := read(4)
	if err != nil || request[0] != 5 || request[1] != 1 {
		return
	}
	host := ""
	switch request[3] {
	case 1:
		b, e := read(4)
		if e != nil {
			return
		}
		host = net.IP(b).String()
	case 3:
		host, err = readString()
		if err != nil {
			return
		}
	default:
		return
	}
	port, err := read(2)
	if err != nil {
		return
	}
	socksEvents.Lock()
	socksEvents.Rows = append(socksEvents.Rows, socksEvent{User: user, Host: host, AddressType: request[3]})
	socksEvents.Unlock()
	// The reserved .invalid host deliberately cannot work without this proxy.
	if host == "crawl-proxy.invalid" || host == "localhost" || strings.HasSuffix(host, ".onion") {
		host = "127.0.0.1"
	}
	if host != "127.0.0.1" {
		return
	}
	remote, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(port)))))
	if err != nil {
		conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()
	if _, err = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
		return
	}
	done := make(chan struct{})
	go func() { io.Copy(remote, conn); remote.Close(); close(done) }()
	io.Copy(conn, remote)
	conn.Close()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func fixtureSOCKSAddress() string {
	if address := os.Getenv("DELVER_WORKSPACE_FIXTURE_SOCKS_ADDR"); address != "" {
		return address
	}
	return "127.0.0.1:0"
}
