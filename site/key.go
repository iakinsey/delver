package site

import (
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
	"net"
	"net/url"
	"strings"
)

// SiteKey groups scheduling identities without changing HTTP origins. An onion
// address, rather than any subdomain served through it, identifies one service.
func Key(value string) string {
	host := strings.TrimSpace(value)
	if strings.Contains(host, "://") {
		if u, e := url.Parse(host); e == nil {
			host = u.Hostname()
		}
	} else if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	if h, e := idna.Lookup.ToASCII(host); e == nil {
		host = h
	}
	if strings.HasSuffix(host, ".onion") {
		parts := strings.Split(host, ".")
		return strings.Join(parts[len(parts)-2:], ".")
	}
	if d, e := publicsuffix.EffectiveTLDPlusOne(host); e == nil {
		return d
	}
	return host
}

// HTTPURL constructs a location for a host or canonical site, including IPv6.
func HTTPURL(host string) string {
	if ip := net.ParseIP(host); ip != nil && strings.Contains(host, ":") {
		host = "[" + ip.String() + "]"
	}
	return "http://" + host + "/"
}
