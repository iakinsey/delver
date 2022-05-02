package config

import (
	"encoding/json"
	"log"
	"runtime"
	"time"
)

type AdversarialConfig struct {
	SubdomainThreshold   int `json:"subdomain_threshold"`
	EnumerationThreshold int `json:"enumeration_threshold"`
}

type HTTPClientConfig struct {
	Timeout    time.Duration `json:"timeout"`
	UserAgent  string        `json:"user_agent"`
	Socks5Url  string        `json:"socks5_url"`
	MaxRetries int           `json:"max_retries"`
}

type RobotsConfig struct {
	Expiration        time.Duration `json:"expiration"`
	ClearExpiredDelay time.Duration `json:"clear_expired_day"`
}

type PersistentMapConfig struct {
	GCInterval          time.Duration `json:"gc_interval"`
	GCDiscardRatio      float64       `json:"gc_discard_ratio"`
	GCErrThreshold      int           `json:"gc_err_threshold"`
	DefaultPrefetchSize int           `json:"default_prefetch_size"`
}

type Config struct {
	WorkerCounts        int                 `json:"worker_counts"`
	DefaultSaveInterval time.Duration       `json:"default_save_interval"`
	CountriesPath       string              `json:"countries_path"`
	CompaniesPath       string              `json:"companies_path"`
	Adversarial         AdversarialConfig   `json:"adversarial"`
	HTTPClient          HTTPClientConfig    `json:"http_client"`
	Robots              RobotsConfig        `json:"robots"`
	PersistentMap       PersistentMapConfig `json:"persistent_map"`
}

func LoadConfig() Config {
	// Put defaults here
	return Config{
		WorkerCounts:        runtime.NumCPU() * 8,
		DefaultSaveInterval: 2 * time.Minute,
		CompaniesPath:       DataFilePath("data", "companies.json"),
		CountriesPath:       DataFilePath("data", "countries.json"),
		Adversarial: AdversarialConfig{
			SubdomainThreshold:   25,
			EnumerationThreshold: 1,
		},
		HTTPClient: HTTPClientConfig{
			Timeout:    10 * time.Second,
			MaxRetries: 1,
			UserAgent:  "delver pre-alpha",
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

func Set(msg json.RawMessage) {
	conf := LoadConfig()

	if err := json.Unmarshal(msg, &conf); err != nil {
		log.Fatalf("failed to parse application config")
	}
	config = conf
}

func Get() Config {
	return config
}
