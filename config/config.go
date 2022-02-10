package config

import (
	"runtime"
	"time"
)

type AdversarialConfig struct {
	SubdomainThreshold   int
	EnumerationThreshold int
}

type HTTPClientConfig struct {
	Timeout    time.Duration
	UserAgent  string
	Socks5Url  string
	MaxRetries int
}

type RobotsConfig struct {
	Expiration        time.Duration
	ClearExpiredDelay time.Duration
}

type PersistentMapConfig struct {
	GCInterval          time.Duration
	GCDiscardRatio      float64
	GCErrThreshold      int
	DefaultPrefetchSize int
}

type AppConfig struct {
	Loaded              bool
	WorkerCounts        int
	DefaultSaveInterval time.Duration
	CountriesPath       string
	CompaniesPath       string
	Adversarial         AdversarialConfig
	HTTPClient          HTTPClientConfig
	Robots              RobotsConfig
	PersistentMap       PersistentMapConfig
}

func LoadConfig() AppConfig {
	// Put defaults here
	return AppConfig{
		Loaded:              true,
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

var config AppConfig = LoadConfig()

func Get() AppConfig {
	return config
}
