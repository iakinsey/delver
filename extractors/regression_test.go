package extractors

import (
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRegressionUnicodeNgrams(t *testing.T) {
	c := message.CompositeAnalysis{Features: map[string]interface{}{"text": "café déjà vu"}}
	r, err := NewNgramExtractor().Perform(nil, c)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r, features.Ngrams{3: [][]string{{"café", "déjà", "vu"}}}) {
		t.Fatalf("Unicode corrupted: %#v", r)
	}
}
func TestRegressionRepeatedSubdomain(t *testing.T) {
	s := NewAdversarialExtractor()
	c := message.CompositeAnalysis{Features: map[string]interface{}{"url": features.URIs{}}}
	c.URI = "https://example.test"
	u := features.URIs{}
	for i := 0; i < 30; i++ {
		u = append(u, "https://same.example.test/page")
	}
	c.Features["url"] = u
	r, err := s.Perform(nil, c)
	if err != nil {
		t.Fatal(err)
	}
	if *r.(features.Adversarial).SubdomainExplosion {
		t.Fatal("one repeated subdomain counted as 25 distinct subdomains")
	}
}
func TestRegressionHTMLLinkForms(t *testing.T) {
	cases := []string{`<a href="/one">one</a><a href="/two">two</a>`, `<A HREF="/one">one</A><a href="/two">two</a>`, `<a href = "/one">one</a><a href="/two">two</a>`, `<a href=/one>one</a><a href="/two">two</a>`}
	for _, html := range cases {
		t.Run(html, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "page")
			os.WriteFile(path, []byte(html), 0600)
			f, _ := os.Open(path)
			defer f.Close()
			c := message.CompositeAnalysis{}
			c.URI = "https://example.test"
			r, err := NewUrlExtractor().Perform(f, c)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(r, features.URIs{"https://example.test/one", "https://example.test/two"}) {
				t.Fatalf("links=%#v", r)
			}
		})
	}
}
