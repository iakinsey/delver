package extractors

import (
	"os"

	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util/fsm"
)

type urlExtractor struct{}

func NewUrlExtractor() Extractor {
	return &urlExtractor{}
}

func (s *urlExtractor) Perform(f *os.File, meta message.FetcherResponse) (interface{}, error) {
	fsm := fsm.NewFSM(fsm.NewDocumentReaderFSM())

	return fsm.Perform(f)
}
