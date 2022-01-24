package extractors

import (
	"bytes"
	"fmt"
	"os"
	"unicode"

	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"golang.org/x/text/unicode/rangetable"
)

const defaultN = 3

var alphanumeric = []*unicode.RangeTable{
	unicode.Letter,
	unicode.Digit,
}
var separator = []*unicode.RangeTable{
	// separators
	unicode.Space,
	unicode.Pattern_White_Space,
	unicode.Prepended_Concatenation_Mark,
	unicode.Hyphen,
	//terminators
	unicode.Terminal_Punctuation,
	unicode.Quotation_Mark,
	unicode.Sentence_Terminal,
	rangetable.New('(', ')', '[', ']', '{', '}', '<', '>'),
}

var terminators = []*unicode.RangeTable{
	unicode.Terminal_Punctuation,
	unicode.Quotation_Mark,
	unicode.Sentence_Terminal,
	rangetable.New('(', ')', '[', ']', '{', '}', '<', '>'),
}

type ngramExtractor struct {
	N int
}

func NewNgramExtractor() Extractor {
	return &ngramExtractor{
		N: defaultN,
	}
}

func (s *ngramExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	feature := make(features.Ngrams)
	var result [][]string
	var ngrams []string
	var buffer bytes.Buffer
	r := '\n'

	//for _, c := range composite.TextContent {
	for i := 0; i <= len(composite.TextContent); i++ {
		if i < len(composite.TextContent) {
			r = rune(composite.TextContent[i])
		} else {
			// Hack: set the last character to a terminator to allow
			// proper procesisng of the remaining runes
			r = '\n'
		}

		if unicode.IsOneOf(alphanumeric, r) {
			buffer.WriteRune(unicode.ToLower(r))
			continue
		} else if buffer.Len() > 0 && unicode.IsOneOf(separator, r) {
			ngrams = append(ngrams, buffer.String())
			buffer.Reset()
		}

		if len(ngrams) == s.N {
			ngramsCopy := ngrams
			result = append(result, ngramsCopy)
			ngrams = ngrams[1:s.N]
		} else if len(ngrams) < s.N && unicode.IsOneOf(terminators, r) {
			ngrams = make([]string, 0)
			buffer.Reset()
		}
	}

	feature[s.N] = result

	return feature, nil
}

func (s *ngramExtractor) Name() string {
	return message.NgramExtractor
}

func (s *ngramExtractor) Requires() []string {
	return []string{
		message.TextExtractor,
	}
}
func (s *ngramExtractor) SetResult(result interface{}, composite *message.CompositeAnalysis) error {
	switch d := result.(type) {
	case features.Ngrams:
		composite.Ngrams = &d
		return nil
	default:
		return fmt.Errorf("NgramExtractor: attempt to cast unknown type")
	}
}
