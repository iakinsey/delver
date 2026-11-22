package config

import (
	"context"
	"encoding/json"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/report"
	"github.com/iakinsey/delver/trap"
	"io/fs"
	"log"
	"runtime"
	"sync"
	"time"
)

const TransformerQueueName = "transformer_queue"

type AdversarialConfig struct {
	SubdomainThreshold   int `json:"subdomain_threshold"`
	EnumerationThreshold int `json:"enumeration_threshold"`
}

type HTTPGate interface {
	Acquire(context.Context, string, int, time.Duration) (func(), error)
}
type HTTPClientConfig struct {
	Protection           *protection.Policy `json:"-"`
	ProtectionCrawl      string             `json:"-"`
	ProtectionRun        string             `json:"-"`
	RestrictedPurgeDepth int                `json:"restricted_purge_depth"`
	Reporter             report.Writer      `json:"-"`
	ExcludedExtensions   []string           `json:"excluded_extensions,omitempty"`
	Flow                 *flow.Controller   `json:"-"`
	ConnectTimeout       time.Duration      `json:"connect_timeout"`
	TLSTimeout           time.Duration      `json:"tls_timeout"`
	HeaderTimeout        time.Duration      `json:"header_timeout"`
	BodyTimeout          time.Duration      `json:"body_timeout"`
	RequireProxy         bool               `json:"require_proxy,omitempty"`
	IsolateProxySessions bool               `json:"isolate_proxy_sessions,omitempty"`
	ProxySessionKey      string             `json:"-"`
	AllowedHostSuffixes  []string           `json:"allowed_host_suffixes,omitempty"`
	RetryDelay           time.Duration      `json:"retry_delay"`
	RetryMaxDelay        time.Duration      `json:"retry_max_delay"`
	AllowPattern         string             `json:"allow_pattern,omitempty"`
	DenyPattern          string             `json:"deny_pattern,omitempty"`
	Scope                string             `json:"scope,omitempty"`
	LocalGate            HTTPGate           `json:"-"`
	GlobalGate           HTTPGate           `json:"-"`
	MaxResponseBytes     int64              `json:"max_response_bytes"`
	PerOriginConcurrency int                `json:"per_origin_concurrency"`
	RequestDelay         time.Duration      `json:"request_delay"`
	RespectRobots        bool               `json:"respect_robots"`
	AllowedDomains       []string           `json:"allowed_domains"`
	Timeout              time.Duration      `json:"timeout"`
	UserAgent            string             `json:"user_agent"`
	Socks5Url            string             `json:"socks5_url"`
	HTTPProxyUrl         string             `json:"http_proxy_url"`
	MaxRetries           int                `json:"max_retries"`
}

type RobotsConfig struct {
	Expiration        time.Duration `json:"expiration"`
	ClearExpiredDelay time.Duration `json:"clear_expired_delay"`
}

type APIConfig struct {
	StaticFiles    fs.FS  `json:"-"`
	OperationsPath string `json:"operations_path"`
	Enabled        bool   `json:"enabled"`
	Address        string `json:"address"`
	AllowCors      bool   `json:"allow_cors"`
	DashDBPath     string `json:"dash_db_path"`
}

type StreamerConfig struct {
	CompositeIndex  string   `json:"composite_index"`
	MetricIndex     string   `json:"metric_index"`
	AllowedOrigins  []string `json:"allowed_origins"`
	Enabled         bool     `json:"enabled"`
	Address         string   `json:"address"`
	SearchAddresses []string `json:"search_addresses"`
}

type PersistentMapConfig struct {
	GCInterval          time.Duration `json:"gc_interval"`
	GCDiscardRatio      float64       `json:"gc_discard_ratio"`
	GCErrThreshold      int           `json:"gc_err_threshold"`
	DefaultPrefetchSize int           `json:"default_prefetch_size"`
}

type MetricsConfig struct {
	Enabled bool `json:"enabled"`
}

type TransformerConfig struct {
	Enabled         []string `json:"enabled"`
	SearchAddresses []string `json:"search_addresses"`
}

type WorkersConfig struct {
	Enabled      bool `json:"enabled"`
	WorkerCounts int  `json:"worker_counts"`
}

type Config struct {
	Traps           *trap.Settings `json:"traps,omitempty"`
	Fetch           *flow.Policy   `json:"fetch,omitempty"`
	RuntimeCrawlID  string         `json:"-"`
	RuntimeRunID    string         `json:"-"`
	ShutdownTimeout time.Duration  `json:"shutdown_timeout"`
	Workers         WorkersConfig  `json:"workers"`
	Metrics         MetricsConfig  `json:"metrics"`
	// TODO maybe this should be in the workers/resources mapping instead?
	DefaultSaveInterval time.Duration       `json:"default_save_interval"`
	CountriesPath       string              `json:"countries_path"`
	CompaniesPath       string              `json:"companies_path"`
	Adversarial         AdversarialConfig   `json:"adversarial"`
	HTTPClient          HTTPClientConfig    `json:"http_client"`
	API                 APIConfig           `json:"api"`
	Streamer            StreamerConfig      `json:"streamer"`
	Robots              RobotsConfig        `json:"robots"`
	PersistentMap       PersistentMapConfig `json:"persistent_map"`
}

func LoadConfig() Config {
	// Put defaults here
	return Config{
		ShutdownTimeout: 30 * time.Second,
		Workers: WorkersConfig{
			Enabled:      true,
			WorkerCounts: runtime.NumCPU() * 8,
		},
		DefaultSaveInterval: 2 * time.Minute,
		CompaniesPath:       DataFilePath("data", "companies.json"),
		CountriesPath:       DataFilePath("data", "countries.json"),
		Metrics: MetricsConfig{
			Enabled: true,
		},
		Adversarial: AdversarialConfig{
			SubdomainThreshold:   25,
			EnumerationThreshold: 1,
		},
		HTTPClient: HTTPClientConfig{
			MaxResponseBytes:     16 << 20,
			PerOriginConcurrency: 2,
			RequestDelay:         100 * time.Millisecond,
			RespectRobots:        true,
			Timeout:              10 * time.Second,
			MaxRetries:           1,
			RetryDelay:           100 * time.Millisecond,
			RetryMaxDelay:        5 * time.Second,
			UserAgent:            "delver pre-alpha",
		},
		API: APIConfig{
			Enabled:    true,
			Address:    ":8080",
			AllowCors:  true,
			DashDBPath: DataFilePath("data", "dash.db"),
		},
		Streamer: StreamerConfig{
			Enabled: false,
			Address: ":26548",
		},
		Robots: RobotsConfig{
			Expiration:        1 * time.Hour,
			ClearExpiredDelay: 1 * time.Hour,
		},
		PersistentMap: PersistentMapConfig{
			GCInterval:          5 * time.Minute,
			GCDiscardRatio:      0.7,
			GCErrThreshold:      2,
			DefaultPrefetchSize: 64,
		},
	}
}

var config Config = LoadConfig()
var configMutex sync.RWMutex

func Set(msg json.RawMessage) {
	conf := LoadConfig()

	if err := json.Unmarshal(msg, &conf); err != nil {
		log.Panicf("failed to parse application config")
	}
	Apply(conf)
}

func Get() Config {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return config
}

func Apply(conf Config) { configMutex.Lock(); defer configMutex.Unlock(); config = conf }
