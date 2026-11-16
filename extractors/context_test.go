package extractors

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
)

type workContext struct {
	context.Context
	checks, limit int
}

func (c *workContext) Err() error {
	c.checks++
	if c.checks >= c.limit {
		return context.Canceled
	}
	return nil
}
func TestBuiltInExtractorsCancelDuringWork(t *testing.T) {
	cases := []struct {
		name      string
		extractor ContextExtractor
		input     string
		features  map[string]interface{}
		checks    int
	}{
		{"text", &textExtractor{}, "<p>" + strings.Repeat("content ", 10000) + "</p>", nil, 6},
		{"title", &titleExtractor{}, "<html>" + strings.Repeat("<p>content</p>", 10000) + "</html>", nil, 6},
		{"url", &urlExtractor{}, strings.Repeat(`<a href="/child">child</a>`, 10000), nil, 6},
		{"ngram", &ngramExtractor{N: 3}, "", map[string]interface{}{"text": strings.Repeat("word ", 10000)}, 6},
		{"country", &countryExtractor{countries: types.CountryRegexes{"USA": regexp.MustCompile("United States")}}, "", map[string]interface{}{"text": strings.Repeat("none ", 10000)}, 20},
		{"company", &companyNameExtractor{companies: []*types.Company{{Identifier: "test", Regex: *regexp.MustCompile("company")}}}, "", map[string]interface{}{"text": strings.Repeat("none ", 10000)}, 20},
		{"adversarial", &adversarialExtractor{}, "", map[string]interface{}{"url": features.URIs{strings.Repeat("a", 1000)}}, 2},
		{"sentiment", NewSentimentExtractor().(ContextExtractor), "", map[string]interface{}{"title": "A useful English title", "language": features.Language{Name: features.LangEnglish}}, 2},
		{"language", &languageExtractor{}, "", map[string]interface{}{"text": strings.Repeat("This is English text. ", 10000)}, 2},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "input")
			require.NoError(t, err)
			defer f.Close()
			_, err = f.WriteString(test.input)
			require.NoError(t, err)
			_, err = f.Seek(0, 0)
			require.NoError(t, err)
			ctx := &workContext{Context: context.Background(), limit: test.checks}
			_, err = test.extractor.PerformContext(ctx, f, message.CompositeAnalysis{FetcherResponse: message.FetcherResponse{FetcherRequest: message.FetcherRequest{URI: "http://example.test/"}}, Features: test.features})
			require.ErrorIs(t, err, context.Canceled)
			require.GreaterOrEqual(t, ctx.checks, test.checks)
		})
	}
}
