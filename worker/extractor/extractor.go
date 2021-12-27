package extractor

import (
	"os"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
)

type Extractor interface {
	Perform(os.File, message.FetcherResponse, types.CompositeAnalysis) error
}
