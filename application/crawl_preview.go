package application

import (
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/util"
	"net/url"
	"regexp"
)

func previewDefinition(raw json.RawMessage) (interface{}, error) {
	var req struct {
		Definition json.RawMessage `json:"definition"`
		URLs       []string        `json:"urls"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	d, err := decodeDefinition(req.Definition)
	if err != nil {
		return nil, err
	}
	var allow, deny *regexp.Regexp
	if d.AllowPattern != "" {
		allow, err = regexp.Compile(d.AllowPattern)
		if err != nil {
			return nil, err
		}
	}
	if d.DenyPattern != "" {
		deny, err = regexp.Compile(d.DenyPattern)
		if err != nil {
			return nil, err
		}
	}
	var suffixes []string
	if d.Network.RestrictHosts {
		suffixes, err = util.NormalizeHostSuffixes(d.Network.AllowedHostSuffixes)
		if err != nil {
			return nil, err
		}
		if len(suffixes) == 0 {
			return nil, fmt.Errorf("allowed hostname suffix required")
		}
	}
	out := []map[string]interface{}{}
	for _, uri := range req.URLs {
		ok := false
		for _, root := range d.Seeds {
			if util.ScopeAllows(d.Scope, root, uri) {
				ok = true
				break
			}
		}
		if d.Scope == "open_web" {
			ok = util.ScopeAllows("open_web", "", uri)
		}
		if allow != nil && !allow.MatchString(uri) {
			ok = false
		}
		if deny != nil && deny.MatchString(uri) {
			ok = false
		}
		hostAllowed := util.URLHostnameAllowed(uri, suffixes)
		if !hostAllowed {
			ok = false
		}
		row := map[string]interface{}{"url": uri, "allowed": ok}
		target, parseErr := url.Parse(uri)
		valid := parseErr == nil && target.Hostname() != "" && (target.Scheme == "http" || target.Scheme == "https")
		if len(d.Seeds) == 0 && len(d.Feeds) > 0 && d.Scope != "open_web" && valid && hostAllowed && (allow == nil || allow.MatchString(uri)) && (deny == nil || !deny.MatchString(uri)) {
			row["reason"] = "Scope unresolved: feed sources not loaded"
		}
		out = append(out, row)
	}
	return out, nil
}
