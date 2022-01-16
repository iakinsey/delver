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
	prevIsSpacing := unicode.IsOneOf(spacing, rune(content[0]))

	dedupeBuf.WriteByte(content[0])

	for i := 1; i < len(content); i++ {
		char := content[i]
		charIsSpacing := unicode.IsOneOf(spacing, rune(char))

		if !(prevIsSpacing && charIsSpacing) {
			dedupeBuf.WriteByte(char)
		}

		prevIsSpacing = charIsSpacing
	}

	return features.TextContent(dedupeBuf.Bytes()), nil
}

func (s *textExtractor) Name() string {
	return message.TextExtractor
}

func (s *textExtractor) Requires() []string {
	return nil
}

func (s *textExtractor) SetResult(result interface{}, composite *message.CompositeAnalysis) error {
	switch d := result.(type) {
	case features.TextContent:
		composite.TextContent = d
		return nil
	default:
		return fmt.Errorf("TextExtractor: attempt to cast unknown type")
	}
}
