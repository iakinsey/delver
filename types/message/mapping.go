package message

import (
	"fmt"

	"github.com/iakinsey/delver/types"
)

func GetMessageTypeMapping(msg interface{}) (types.MessageType, error) {
	switch msg.(type) {
	case FetcherRequest:
		return types.FetcherRequestType, nil
	case FetcherResponse:
		return types.FetcherResponseType, nil
	case types.CompositeAnalysis:
		return types.CompositeAnalysisType, nil
	default:
		return types.NullMessage, fmt.Errorf("unmappable message type")
	}
}
