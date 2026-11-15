package extractors

import (
	"context"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"golang.org/x/net/html"
	"io"
	"net/url"
	"os"
	"strings"
)

type urlExtractor struct{}

func NewUrlExtractor() Extractor { return &urlExtractor{} }
func (s *urlExtractor) Perform(f *os.File, c message.CompositeAnalysis) (interface{}, error) {
	return s.PerformContext(context.Background(), f, c)
}
func (s *urlExtractor) PerformContext(ctx context.Context, f *os.File, c message.CompositeAnalysis) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	origin := c.EffectiveURI
	if origin == "" {
		origin = c.URI
	}
	base, err := url.Parse(origin)
	if err != nil {
		return nil, err
	}
	tokenizer := html.NewTokenizer(contextReader{ctx, f})
	var refs []string
	hasBase := false
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			if err := tokenizer.Err(); err != io.EOF {
				return nil, err
			}
			break
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		for _, attr := range token.Attr {
			if attr.Key != "href" {
				continue
			}
			if token.Data == "base" && !hasBase {
				if u, err := base.Parse(attr.Val); err == nil {
					base = u
					hasBase = true
				}
			} else if token.Data == "a" {
				refs = append(refs, attr.Val)
			}
		}
	}
	result := features.URIs{}
	seen := map[string]bool{}
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		u, err := base.Parse(strings.TrimSpace(ref))
		if err != nil {
			continue
		}
		if u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
			continue
		}
		u.Fragment = ""
		u.Host = strings.ToLower(u.Host)
		key := u.String()
		if !seen[key] {
			seen[key] = true
			result = append(result, key)
		}
	}
	return result, nil
}
func (s *urlExtractor) Name() string       { return features.UrlField }
func (s *urlExtractor) Requires() []string { return nil }
