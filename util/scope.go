package util

import (
	"context"
	"golang.org/x/net/publicsuffix"
	"net"
	"net/url"
	"strings"
)

func RegistrableDomain(host string) string {
	host = strings.ToLower(host)
	if net.ParseIP(host) != nil {
		return host
	}
	d, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return host
	}
	return d
}
func ScopeAllows(scope, root, target string) bool {
	a, err := url.Parse(root)
	if err != nil {
		return false
	}
	b, err := url.Parse(target)
	if err != nil || b.Hostname() == "" || b.Scheme != "http" && b.Scheme != "https" {
		return false
	}
	switch scope {
	case "open_web", "":
		return true
	case "same_domain":
		return RegistrableDomain(a.Hostname()) == RegistrableDomain(b.Hostname())
	case "same_origin":
		port := func(u *url.URL) string {
			if u.Port() != "" {
				return u.Port()
			}
			if u.Scheme == "https" {
				return "443"
			}
			return "80"
		}
		return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
	}
	return false
}

type requestPolicyKey struct{}

func WithHTTPPolicy(ctx context.Context, policy func(*url.URL) error) context.Context {
	return context.WithValue(ctx, requestPolicyKey{}, policy)
}

// WithoutHTTPPolicy keeps cancellation while allowing robots metadata to be fetched
// independently of page-path inclusion rules.
func WithoutHTTPPolicy(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestPolicyKey{}, false)
}
