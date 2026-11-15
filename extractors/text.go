package extractors

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"os"
	"unicode"

	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/microcosm-cc/bluemonday"
)

type textExtractor struct{}

func NewTextExtractor() Extractor {
	return &textExtractor{}
}

var spacing = []*unicode.RangeTable{
	unicode.Space,
	unicode.Pattern_White_Space,
}

func (s *textExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	return s.PerformContext(context.Background(), f, composite)
}
func (s *textExtractor) PerformContext(ctx context.Context, f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p := bluemonday.StripTagsPolicy()
	buf := p.SanitizeReader(contextReader{ctx, f})

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if buf.Len() == 0 {
		return nil, fmt.Errorf("request has no content: %s", composite.RequestID)
	}

	content := html.UnescapeString(buf.String())
	var dedupeBuf bytes.Buffer
	prevIsSpacing := false
	for index, char := range content {
		if index%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		isSpacing := unicode.IsOneOf(spacing, char)
		if !(prevIsSpacing && isSpacing) {
			dedupeBuf.WriteRune(char)
		}
		prevIsSpacing = isSpacing
	}

	return dedupeBuf.String(), nil
}

func (s *textExtractor) Name() string {
	return features.TextField
}

func (s *textExtractor) Requires() []string {
	return nil
}
