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

type AppConfig struct {
	Loaded        bool
	WorkerCounts  int
	CountriesPath string
	CompaniesPath string
	Adversarial   AdversarialConfig
	HTTPClient    HTTPClientConfig
	Robots        RobotsConfig
}

func LoadConfig() AppConfig {
	// Put defaults here
	return AppConfig{
		Loaded:        true,
		WorkerCounts:  runtime.NumCPU() * 8,
		CompaniesPath: DataFilePath("data", "companies.json"),
		CountriesPath: DataFilePath("data", "countries.json"),
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
	}
}

var config AppConfig = LoadConfig()

func Get() AppConfig {
	return config
}
