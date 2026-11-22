package util

import "github.com/iakinsey/delver/site"

// SiteKey is the canonical domain-level scheduling identity.
func SiteKey(value string) string { return site.Key(value) }
