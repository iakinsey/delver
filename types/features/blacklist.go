package features

type Blacklist struct {
	HasBlacklistedKeyword bool `json:"has_blacklisted_keyword"`
	HasManualBlacklist    bool `json:"has_manual_blacklist"`
}
