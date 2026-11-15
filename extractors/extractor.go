package extractors

import (
	"context"
	"os"

	"github.com/iakinsey/delver/types/message"
)

type Extractor interface {
	Perform(*os.File, message.CompositeAnalysis) (interface{}, error)
	Requires() []string
	Name() string
}

type ContextExtractor interface {
	PerformContext(context.Context, *os.File, message.CompositeAnalysis) (interface{}, error)
}
