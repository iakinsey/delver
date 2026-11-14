package extractors

import (
	"bytes"
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
	p := bluemonday.StripTagsPolicy()
	buf := p.SanitizeReader(f)

	if buf.Len() == 0 {
		return nil, fmt.Errorf("request has no content: %s", composite.RequestID)
	}

	content := html.UnescapeString(buf.String())
	var dedupeBuf bytes.Buffer
	prevIsSpacing := false
	for _, char := range content {
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
