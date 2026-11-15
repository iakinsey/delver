package util

import (
	"fmt"
	"golang.org/x/net/proxy"
	"net"
	"net/url"
	"strings"
)

func ParseSOCKSProxy(raw string) (string, *proxy.Auth, error) {
	address := raw
	var auth *proxy.Auth
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "socks5" && u.Scheme != "socks5h" || u.Host == "" || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
			return "", nil, fmt.Errorf("invalid SOCKS5 proxy URL")
		}
		address = u.Host
		if u.User != nil {
			password, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: password}
		}
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		return "", nil, fmt.Errorf("SOCKS5 proxy requires host:port")
	}
	return address, auth, nil
}
