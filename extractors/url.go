package extractors

import (
	"fmt"
	"net/url"
	"os"

	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/util/fsm"
)

type urlExtractor struct{}

func NewUrlExtractor() Extractor {
	return &urlExtractor{}
}

func (s *urlExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	base, err := url.Parse(composite.URI)

	if err != nil {
		return nil, err
	}

	fsm := fsm.NewFSM(fsm.NewDocumentReaderFSM())
	urls, err := fsm.Perform(f)

	if err != nil {
		return nil, err
	}

	result := util.ResolveUrls(base, util.DedupeStrSlice(urls))

	return features.URIs(result), nil
}

func (s *urlExtractor) Name() string {
	return message.UrlExtractor
}

func (s *urlExtractor) Requires() []string {
	return nil
}

func (s *urlExtractor) SetResult(result interface{}, composite *message.CompositeAnalysis) error {
	switch d := result.(type) {
	case features.URIs:
		composite.URIs = d
		return nil
	default:
		return fmt.Errorf("TextExtractor: attempt to cast unknown type")
	}
}
