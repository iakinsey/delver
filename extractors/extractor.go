package extractors

import (
	"os"

	"github.com/iakinsey/delver/types/message"
)

type Extractor interface {
	Perform(*os.File, message.FetcherResponse, message.CompositeAnalysis) (interface{}, error)
	Requires() []string
	Name() string
}
