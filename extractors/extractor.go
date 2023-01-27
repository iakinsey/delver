package extractors

import (
	"os"

	"github.com/iakinsey/delver/types/message"
)

type Extractor interface {
	Perform(*os.File, message.CompositeAnalysis) (interface{}, error)
	Requires() []string
	Name() string
}
