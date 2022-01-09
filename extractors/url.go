package extractors

import (
	"net/url"
	"os"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/util/fsm"
)

type urlExtractor struct{}

func NewUrlExtractor() Extractor {
	return &urlExtractor{}
}

func (s *urlExtractor) Perform(f *os.File, meta message.FetcherResponse, composite types.CompositeAnalysis) (interface{}, error) {
	base, err := url.Parse(meta.URI)

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
	return types.UrlExtractor
}

func (s *urlExtractor) Requires() []string {
	return nil
}
