package util

import (
	"fmt"
	"golang.org/x/net/idna"
	"net"
	"net/url"
	"strings"
)

// NormalizeHostSuffixes validates labels without DNS lookups. A suffix matches
// the named host and its subdomains, never a lookalike ending in another label.
func NormalizeHostSuffixes(values []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(value), "."), ".")
		host, err := idna.Lookup.ToASCII(value)
		host = strings.ToLower(host)
		if err != nil || host == "" || len(host) > 253 || strings.ContainsAny(host, "/:@?# ") {
			return nil, fmt.Errorf("hostname suffixes must contain hostnames without schemes, ports, or paths")
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return nil, fmt.Errorf("invalid hostname suffix")
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return nil, fmt.Errorf("invalid hostname suffix")
				}
			}
		}
		if !seen[host] {
			seen[host] = true
			out = append(out, host)
		}
	}
	return out, nil
}
func HostnameAllowed(u *url.URL, suffixes []string) bool {
	if u == nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if len(suffixes) == 0 {
		return true
	}
	host, err := idna.Lookup.ToASCII(strings.TrimSuffix(u.Hostname(), "."))
	if err != nil {
		return false
	}
	host = strings.ToLower(host)
	for _, suffix := range suffixes {
		if host == suffix || net.ParseIP(suffix) == nil && strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
func URLHostnameAllowed(raw string, suffixes []string) bool {
	u, err := url.Parse(raw)
	return err == nil && HostnameAllowed(u, suffixes)
}
