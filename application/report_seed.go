package application

import (
	"context"
	"encoding/json"
	"github.com/iakinsey/delver/report"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"net"
	"net/url"
	"regexp"
)

func seedReport(ctx context.Context, p *preparedApplication) error {
	c := p.app.Config.HTTPClient
	var allow, deny *regexp.Regexp
	if c.AllowPattern != "" {
		allow = regexp.MustCompile(c.AllowPattern)
	}
	if c.DenyPattern != "" {
		deny = regexp.MustCompile(c.DenyPattern)
	}
	for _, w := range p.workerDefinitions() {
		if w.Type != "fixed_seed_publisher" {
			continue
		}
		var params struct {
			URIs []string `json:"uris"`
		}
		if e := json.Unmarshal(w.Parameters, &params); e != nil {
			return e
		}
		records := []report.URL{}
		for _, uri := range params.URIs {
			if !util.URLHostnameAllowed(uri, c.AllowedHostSuffixes) || util.URLFileExcluded(uri, c.ExcludedExtensions) || allow != nil && !allow.MatchString(uri) || deny != nil && deny.MatchString(uri) {
				continue
			}
			records = append(records, report.URL{URI: uri})
		}
		if e := p.reporting.Discover(ctx, records); e != nil {
			return e
		}
	}
	return nil
}

// Historical links use the saved run's policies; unreachable/deleted history is
// explicitly partial rather than reconstructed from a single parent pointer.
func reportLinkAllowed(d CrawlDefinition, c message.CompositeAnalysis, uri string) bool {
	if d.Network.RestrictHosts && !util.URLHostnameAllowed(uri, d.Network.AllowedHostSuffixes) {
		return false
	}
	extensions, e := util.NormalizeExtensions(d.ExcludedExtensions)
	if e != nil || util.URLFileExcluded(uri, extensions) {
		return false
	}
	if d.AllowPattern != "" && !regexp.MustCompile(d.AllowPattern).MatchString(uri) || d.DenyPattern != "" && regexp.MustCompile(d.DenyPattern).MatchString(uri) {
		return false
	}
	sourceRaw := c.URI
	if c.EffectiveURI != "" {
		sourceRaw = c.EffectiveURI
	}
	source, _ := url.Parse(sourceRaw)
	target, _ := url.Parse(uri)
	if source == nil || target == nil {
		return false
	}
	depth, hops := c.Depth+1, c.Hops
	domain := func(host string) string {
		if net.ParseIP(host) != nil {
			return host
		}
		return util.GetSLDAndTLD(host)
	}
	if domain(source.Hostname()) != domain(target.Hostname()) {
		depth = 0
		hops++
	}
	if depth > d.MaxDepth || hops > d.MaxHops {
		return false
	}

	if !util.ScopeAllows(d.Scope, c.URI, uri) {
		return false
	}
	return true
}

func resolveReportURL(raw, base string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return ""
	}
	b, e := url.Parse(base)
	if e != nil {
		return ""
	}
	return b.ResolveReference(u).String()
}
