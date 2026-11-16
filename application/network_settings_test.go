package application

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestExpertNetworkDurationStrings(t *testing.T) {
	a, err := Decode(strings.NewReader(`{"config":{"http_client":{"retry_delay":"1s","retry_max_delay":"30s"}},"resources":[],"workers":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if a.Config.HTTPClient.RetryDelay != time.Second || a.Config.HTTPClient.RetryMaxDelay != 30*time.Second {
		t.Fatal("expert retry durations were not decoded", a.Config.HTTPClient)
	}
}

func TestGenericNetworkSettingsCompileAndLegacyDefaults(t *testing.T) {
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://service.onion/"}
	d.Network = NetworkSettings{}
	a, err := CompileCrawl(t.TempDir(), "legacy", d)
	if err != nil {
		t.Fatal(err)
	}
	if a.Config.HTTPClient.Timeout != 10*time.Second || a.Config.HTTPClient.MaxRetries != 1 {
		t.Fatal("legacy defaults changed")
	}
	retries := 0
	d.Proxy = ProxySettings{Mode: "socks5", URL: "socks5h://127.0.0.1:9050"}
	d.Network = NetworkSettings{RequestTimeout: "120s", MaxRetries: &retries, RetryDelay: "1s", RetryMaxDelay: "30s", UserAgent: "ResearchCrawler/1.0", RequireProxy: true, RestrictHosts: true, AllowedHostSuffixes: []string{".ONION"}, IsolateProxySessions: true}
	a, err = CompileCrawl(t.TempDir(), "private", d)
	if err != nil {
		t.Fatal(err)
	}
	c := a.Config.HTTPClient
	if c.Timeout != 120*time.Second || c.MaxRetries != 0 || !c.RequireProxy || !c.IsolateProxySessions || c.UserAgent != "ResearchCrawler/1.0" || len(c.AllowedHostSuffixes) != 1 || c.AllowedHostSuffixes[0] != "onion" || c.RetryDelay != time.Second || c.RetryMaxDelay != 30*time.Second {
		t.Fatal(c)
	}
	for _, worker := range a.Workers {
		if worker.Type == "frontier_accumulator" || worker.Type == "frontier_admission" {
			var p map[string]interface{}
			json.Unmarshal(worker.Parameters, &p)
			suffixes := p["allowed_host_suffixes"].([]interface{})
			if suffixes[0] != "onion" {
				t.Fatal("discovery did not receive policy", p)
			}
		}
	}
}
func TestGenericNetworkValidation(t *testing.T) {
	for name, mutate := range map[string]func(*CrawlDefinition){
		"timeout":                func(d *CrawlDefinition) { d.Network.RequestTimeout = "0s" },
		"negative timeout":       func(d *CrawlDefinition) { d.Network.RequestTimeout = "-1s" },
		"backoff":                func(d *CrawlDefinition) { d.Network.RetryDelay = "0s" },
		"backoff maximum":        func(d *CrawlDefinition) { d.Network.RetryDelay = "10s"; d.Network.RetryMaxDelay = "1s" },
		"retries":                func(d *CrawlDefinition) { n := 11; d.Network.MaxRetries = &n },
		"negative retries":       func(d *CrawlDefinition) { n := -1; d.Network.MaxRetries = &n },
		"header injection":       func(d *CrawlDefinition) { d.Network.UserAgent = "agent\r\nInjected: yes" },
		"proxy required":         func(d *CrawlDefinition) { d.Network.RequireProxy = true },
		"session requires socks": func(d *CrawlDefinition) { d.Network.IsolateProxySessions = true },
		"session cannot replace auth": func(d *CrawlDefinition) {
			d.Proxy = ProxySettings{Mode: "socks5", URL: "socks5://user:pass@127.0.0.1:9050"}
			d.Network.IsolateProxySessions = true
		},
		"empty suffix": func(d *CrawlDefinition) { d.Network.RestrictHosts = true },
		"invalid suffix": func(d *CrawlDefinition) {
			d.Network.RestrictHosts = true
			d.Network.AllowedHostSuffixes = []string{"https://onion"}
		},
		"seed outside": func(d *CrawlDefinition) {
			d.Network.RestrictHosts = true
			d.Network.AllowedHostSuffixes = []string{"internal.example"}
		},
		"feed outside": func(d *CrawlDefinition) {
			d.Network.RestrictHosts = true
			d.Network.AllowedHostSuffixes = []string{"onion"}
			d.Feeds = []string{"https://public.example/feed"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := DefaultCrawlDefinition()
			d.Seeds = []string{"http://service.onion/"}
			mutate(&d)
			if _, err := CompileCrawl(t.TempDir(), name, d); err == nil {
				t.Fatal("invalid network settings accepted")
			}
		})
	}
}

func TestRobotsSettingDefaultsAndExplicitChoice(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value *bool
		want  bool
	}{
		{"legacy omitted", nil, true}, {"respect", boolPointer(true), true}, {"ignore", boolPointer(false), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := DefaultCrawlDefinition()
			d.Seeds = []string{"http://example.test/"}
			d.Network.RespectRobots = tc.value
			a, err := CompileCrawl(t.TempDir(), tc.name, d)
			if err != nil {
				t.Fatal(err)
			}
			if a.Config.HTTPClient.RespectRobots != tc.want {
				t.Fatal("robots setting was not applied")
			}
		})
	}
}
func boolPointer(value bool) *bool { return &value }
