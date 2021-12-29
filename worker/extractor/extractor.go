package extractor

import (
	"os"

	"github.com/iakinsey/delver/types/message"
)

type Extractor interface {
	Perform(*os.File, message.FetcherResponse) (interface{}, error)
}
