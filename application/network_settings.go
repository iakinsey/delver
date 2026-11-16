package application

import (
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/util"
	"strings"
	"time"
)

// NetworkSettings are protocol-independent controls for slow services, private
// namespaces and session-aware SOCKS proxies. No network-specific client runs here.
type NetworkSettings struct {
	RespectRobots        *bool    `json:"respect_robots,omitempty"`
	RequestTimeout       string   `json:"request_timeout"`
	MaxRetries           *int     `json:"max_retries,omitempty"`
	RetryDelay           string   `json:"retry_delay"`
	RetryMaxDelay        string   `json:"retry_max_delay"`
	UserAgent            string   `json:"user_agent"`
	RequireProxy         bool     `json:"require_proxy"`
	RestrictHosts        bool     `json:"restrict_hosts"`
	AllowedHostSuffixes  []string `json:"allowed_host_suffixes"`
	IsolateProxySessions bool     `json:"isolate_proxy_sessions"`
}

func DefaultNetworkSettings() NetworkSettings {
	retries := 1
	respect := true
	return NetworkSettings{RespectRobots: &respect, RequestTimeout: "10s", MaxRetries: &retries, RetryDelay: "100ms", RetryMaxDelay: "5s", UserAgent: "delver pre-alpha", AllowedHostSuffixes: []string{}}
}
func applyNetworkSettings(c *config.HTTPClientConfig, n NetworkSettings) error {
	defaults := DefaultNetworkSettings()
	if n.RequestTimeout == "" {
		n.RequestTimeout = defaults.RequestTimeout
	}
	if n.MaxRetries == nil {
		n.MaxRetries = defaults.MaxRetries
	}
	if n.RetryDelay == "" {
		n.RetryDelay = defaults.RetryDelay
	}
	if n.RetryMaxDelay == "" {
		n.RetryMaxDelay = defaults.RetryMaxDelay
	}
	if n.UserAgent == "" {
		n.UserAgent = defaults.UserAgent
	}
	timeout, err := time.ParseDuration(n.RequestTimeout)
	if err != nil || timeout <= 0 {
		return fmt.Errorf("request timeout must be a positive duration")
	}
	retry, err := time.ParseDuration(n.RetryDelay)
	if err != nil || retry <= 0 {
		return fmt.Errorf("retry delay must be a positive duration")
	}
	maximum, err := time.ParseDuration(n.RetryMaxDelay)
	if err != nil || maximum < retry {
		return fmt.Errorf("maximum retry delay must cover the initial retry delay")
	}
	if *n.MaxRetries < 0 || *n.MaxRetries > 10 {
		return fmt.Errorf("retries must be between zero and ten")
	}
	if strings.ContainsAny(n.UserAgent, "\r\n") || strings.TrimSpace(n.UserAgent) == "" {
		return fmt.Errorf("user agent must be a nonempty single line")
	}
	var suffixes []string
	if n.RestrictHosts {
		suffixes, err = util.NormalizeHostSuffixes(n.AllowedHostSuffixes)
		if err != nil {
			return err
		}
		if len(suffixes) == 0 {
			return fmt.Errorf("enter at least one allowed hostname suffix or turn off hostname restrictions")
		}
	}
	c.Timeout, c.MaxRetries, c.RetryDelay, c.RetryMaxDelay, c.UserAgent = timeout, *n.MaxRetries, retry, maximum, n.UserAgent
	c.RespectRobots = n.RespectRobots == nil || *n.RespectRobots
	c.RequireProxy, c.IsolateProxySessions, c.AllowedHostSuffixes = n.RequireProxy, n.IsolateProxySessions, suffixes
	return nil
}
