// Package protection implements shared, persistent workspace avoidance policies.
package protection

import (
	"bufio"
	"fmt"
	"golang.org/x/net/idna"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Entry struct {
	Value  string `json:"value"`
	Domain bool   `json:"domain"`
}
type List struct {
	ActivationID       string    `json:"activation_id,omitempty"`
	ActivationRevision uint64    `json:"activation_revision,omitempty"`
	EntryCount         int       `json:"entry_count,omitempty"`
	Version            string    `json:"version"`
	Attempted          time.Time `json:"attempted"`
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Enabled            bool      `json:"enabled"`
	URL                string    `json:"url,omitempty"`
	Text               string    `json:"text,omitempty"`
	Refresh            string    `json:"refresh"`
	Updated            time.Time `json:"updated"`
	Error              string    `json:"error,omitempty"`
	Entries            []Entry   `json:"entries"`
}
type Rule struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Enabled bool     `json:"enabled"`
	Phrases []string `json:"phrases"`
}

func DefaultRules() []Rule {
	return []Rule{
		{"distribution-material", "Explicit material solicitation", true, []string{"buy child sexual abuse material"}},
		{"distribution-videos", "Explicit video solicitation", true, []string{"download child sexual abuse videos"}},
	}
}
func NormalizeURL(raw string) (string, error) { return normalizeURL(raw, false) }

// Destination identity excludes HTTP credentials; matching and cleanup must
// cover authenticated requests without storing credentials in an incident.
func NormalizeDestination(raw string) (string, error) { return normalizeURL(raw, true) }
func normalizeURL(raw string, credentials bool) (string, error) {
	raw = strings.TrimSpace(raw)
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil && !credentials || u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("HTTP(S) URL without credentials required")
	}
	u.User = nil
	port := u.Port()
	if port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("invalid URL port")
		}
	}
	rawHost := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	h := rawHost
	if ip := net.ParseIP(rawHost); ip != nil {
		h = ip.String()
	} else {
		h, e = idna.Lookup.ToASCII(rawHost)
	}
	if e != nil {
		return "", e
	}
	u.Host = h
	if strings.Contains(h, ":") {
		u.Host = "[" + h + "]"
	}
	if port != "" && !(u.Scheme == "http" && port == "80" || u.Scheme == "https" && port == "443") {
		u.Host = net.JoinHostPort(h, port)
	}
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}
func ParseList(text string) ([]Entry, error) {
	if len(text) > 64<<20 {
		return nil, fmt.Errorf("list exceeds 64 MiB")
	}
	out := []Entry{}
	seen := map[string]bool{}
	scan := bufio.NewScanner(strings.NewReader(text))
	scan.Buffer(make([]byte, 4096), 16384)
	line := 0
	for scan.Scan() {
		line++
		s := strings.TrimSpace(scan.Text())
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		entry := Entry{Domain: !strings.Contains(s, "://")}
		var e error
		if entry.Domain {
			if strings.ContainsAny(s, " /?#*@\\\t\r") {
				return nil, fmt.Errorf("line %d: domain or HTTP(S) URL required", line)
			}
			entry.Value, e = idna.Lookup.ToASCII(strings.TrimSuffix(strings.ToLower(s), "."))
			if e == nil && (entry.Value == "" || strings.Contains(entry.Value, ":")) {
				e = fmt.Errorf("domain required")
			}
		} else {
			entry.Value, e = NormalizeURL(s)
		}
		if e != nil {
			return nil, fmt.Errorf("line %d: %w", line, e)
		}
		key := fmt.Sprint(entry.Domain) + entry.Value
		if !seen[key] {
			seen[key] = true
			out = append(out, entry)
		}
		if len(out) > 2000000 {
			return nil, fmt.Errorf("list exceeds 2000000 entries")
		}
	}
	if e := scan.Err(); e != nil {
		return nil, fmt.Errorf("line %d: %w", line+1, e)
	}
	return out, nil
}
func (e Entry) Matches(raw string) bool {
	normal, err := NormalizeDestination(raw)
	if err != nil {
		return false
	}
	if !e.Domain {
		return normal == e.Value
	}
	u, _ := url.Parse(normal)
	h := u.Hostname()
	return h == e.Value || strings.HasSuffix(h, "."+e.Value)
}
