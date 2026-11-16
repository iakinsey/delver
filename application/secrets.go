package application

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/iakinsey/delver/config"
)

const redacted = "__REDACTED__"

func configValue(app config.Application) interface{} {
	data, _ := json.Marshal(app)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value interface{}
	decoder.Decode(&value)
	return value
}
func secretKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "password") || strings.Contains(key, "secret") || strings.Contains(key, "token") || key == "authorization" || key == "api_key" || key == "apikey" || key == "http_proxy_url" || key == "socks5_url"
}
func sensitiveURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		return false
	}
	if u.User != nil {
		return true
	}
	for key := range u.Query() {
		if secretKey(key) {
			return true
		}
	}
	return false
}
func redactValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, child := range v {
			// Proxy addresses are editable configuration, including their login.
			if key == "http_proxy_url" || key == "socks5_url" {
				continue
			}
			if key == "proxy" {
				if proxy, ok := child.(map[string]interface{}); ok {
					address, present := proxy["url"]
					delete(proxy, "url")
					redactValue(proxy)
					if present {
						proxy["url"] = address
					}
					continue
				}
			}
			if secretKey(key) && child != "" && child != nil {
				v[key] = redacted
			} else {
				v[key] = redactValue(child)
			}
		}
	case []interface{}:
		for i, child := range v {
			v[i] = redactValue(child)
		}
	case string:
		if sensitiveURL(v) {
			return redacted
		}
	}
	return value
}
func restoreValue(value, previous interface{}) interface{} {
	if text, ok := value.(string); ok && text == redacted {
		return previous
	}
	switch v := value.(type) {
	case map[string]interface{}:
		old, _ := previous.(map[string]interface{})
		for key, child := range v {
			v[key] = restoreValue(child, old[key])
		}
	case []interface{}:
		old, _ := previous.([]interface{})
		for i, child := range v {
			var prior interface{}
			if i < len(old) {
				prior = old[i]
			}
			if named, ok := child.(map[string]interface{}); ok {
				if name, ok := named["name"].(string); ok {
					for _, candidate := range old {
						if object, ok := candidate.(map[string]interface{}); ok && object["name"] == name {
							prior = candidate
							break
						}
					}
				}
			}
			v[i] = restoreValue(child, prior)
		}
	}
	return value
}
func restoreConfiguration(data []byte, base config.Application) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(restoreValue(value, configValue(base)))
}

func sanitizeText(text string, applications ...config.Application) string {
	var values []string
	var collect func(interface{}, bool)
	collect = func(value interface{}, secret bool) {
		switch v := value.(type) {
		case map[string]interface{}:
			for key, child := range v {
				collect(child, secret || secretKey(key))
			}
		case []interface{}:
			for _, child := range v {
				collect(child, secret)
			}
		case string:
			if secret && v != "" {
				values = append(values, v)
			}
			if sensitiveURL(v) {
				values = append(values, v)
				u, _ := url.Parse(v)
				if u.User != nil {
					values = append(values, u.User.String())
					if password, ok := u.User.Password(); ok && password != "" {
						values = append(values, password)
					}
				}
				for key, items := range u.Query() {
					if secretKey(key) {
						values = append(values, items...)
					}
				}
			}
		}
	}
	for _, app := range applications {
		collect(configValue(app), false)
	}
	for _, value := range values {
		if value != "" {
			text = strings.ReplaceAll(text, value, redacted)
		}
	}
	return text
}
func (s *Supervisor) safeError(text string) string {
	apps := []config.Application{s.state.Active}
	if s.state.Draft != nil {
		apps = append(apps, *s.state.Draft)
	}
	if s.state.Previous != nil {
		apps = append(apps, *s.state.Previous)
	}
	return sanitizeText(text, apps...)
}
