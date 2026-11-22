package util

import "testing"

func TestSiteKey(t *testing.T) {
	for in, want := range map[string]string{"https://a.Example.CO.UK:8443/path": "example.co.uk", "a.service.onion:80": "service.onion", "service.onion.": "service.onion", "127.0.0.1:99": "127.0.0.1", "[2001:db8::1]:80": "2001:db8::1", "a.blogspot.com": "a.blogspot.com", "https://b.xn--bcher-kva.de/": "xn--bcher-kva.de"} {
		if got := SiteKey(in); got != want {
			t.Errorf("%q: %q != %q", in, got, want)
		}
	}
}
