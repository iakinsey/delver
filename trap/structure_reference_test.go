package trap

import (
	"context"
	"github.com/iakinsey/delver/site"
	"io"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
)
import "golang.org/x/net/html"

func observeReference(ctx context.Context, r io.Reader, raw string, links []string) (Observation, error) {
	o := Observation{URI: raw, Site: site.Key(raw), Signature: make([]uint64, 64), Patterns: []uint64{}, Destinations: []string{}}
	for i := range o.Signature {
		o.Signature[i] = ^uint64(0)
	}
	z := html.NewTokenizer(io.LimitReader(r, 16<<20))
	stack := []string{}
	overflow := 0
	shingles := map[uint64]bool{}
	for o.Tokens < 100000 {
		if e := ctx.Err(); e != nil {
			return o, e
		}
		kind := z.Next()
		if kind == html.ErrorToken {
			if e := z.Err(); e != io.EOF {
				return o, e
			}
			break
		}
		if kind != html.StartTagToken && kind != html.EndTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		token := z.Token()
		if token.Data == "script" || token.Data == "style" {
			continue
		}
		o.Tokens++
		if kind == html.EndTagToken {
			if overflow > 0 {
				overflow--
				continue
			}
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i] == token.Data {
					stack = stack[:i]
					break
				}
			}
			continue
		}
		path := strings.Join(stack, "/") + "/" + token.Data
		shingles[hash(path)] = true
		// Ancestry shingles retain local structure without encoding sibling order.
		// Moving a complete section therefore does not change its fingerprint.
		start := max(0, len(stack)-3)
		shingles[hash(strings.Join(stack[start:], ">")+">"+token.Data)] = true
		if len(shingles) > 4096 {
			break
		}
		if kind == html.StartTagToken && !voidTag(token.Data) {
			if len(stack) < 64 && overflow == 0 {
				stack = append(stack, token.Data)
			} else {
				overflow++
			}
		}
	}
	for h := range shingles {
		for i := range o.Signature {
			v := mix(h + uint64(i)*0x9e3779b97f4a7c15)
			if v < o.Signature[i] {
				o.Signature[i] = v
			}
		}
	}
	targets := map[uint64]bool{}
	patterns := map[uint64]bool{}
	dest := map[string]bool{}
	crossSite := 0
	for _, u := range links {
		if o.Links >= 10000 {
			break
		}
		if _, e := url.Parse(u); e != nil {
			continue
		}
		o.Links++
		if h := hash(u); !targets[h] && len(o.Targets) < 128 {
			targets[h] = true
			o.Targets = append(o.Targets, h)
		}
		patterns[Pattern(u)] = true
		key := site.Key(u)
		if key != o.Site {
			crossSite++
			if !dest[key] && len(o.Destinations) < 128 {
				dest[key] = true
				o.Destinations = append(o.Destinations, key)
			}
		}
	}
	o.CrossSiteLinks = float64(crossSite) / float64(max(1, o.Links))
	for p := range patterns {
		o.Patterns = append(o.Patterns, p)
	}
	sort.Slice(o.Patterns, func(i, j int) bool { return o.Patterns[i] < o.Patterns[j] })
	if len(o.Patterns) > 128 {
		o.Patterns = o.Patterns[:128]
	}

	return o, nil
}

func TestStructureReference(t *testing.T) {
	for _, body := range []string{"", "plain", "<div><a href='/'>fixture</a></div>", strings.Repeat("<section><b>x</b></section>", 30000), "<div><script><b>x</script><style>style</style><img><p>&broken", strings.Repeat("<div>", 100) + strings.Repeat("</div>", 110), strings.Repeat("x", ObservationByteLimit-3) + "<a href='/x'>"} {
		compareStructure(t, body)
	}
}
func compareStructure(t *testing.T, body string) {
	t.Helper()
	links := []string{"http://a.invalid/a/1", "http://b.invalid/b?q=2"}
	a, e := Observe(context.Background(), strings.NewReader(body), "http://a.invalid/", links)
	b, f := observeReference(context.Background(), strings.NewReader(body), "http://a.invalid/", links)
	if e != f || !reflect.DeepEqual(a, b) {
		t.Fatalf("structure differs: %v %v; tokens %d/%d", e, f, a.Tokens, b.Tokens)
	}
}
func FuzzStructureReference(f *testing.F) {
	for _, s := range []string{"<div><p>x</p></div>", "<script><a>x</script>", "&amp;"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 1<<20 {
			t.Skip()
		}
		compareStructure(t, s)
	})
}
