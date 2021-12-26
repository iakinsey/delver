package message

import (
	"fmt"

	"github.com/iakinsey/delver/types"
)

func GetMessageTypeMapping(msg interface{}) (types.MessageType, error) {
	switch msg.(type) {
	case FetcherRequest:
		return types.FetchRequest, nil
	case FetcherResponse:
		return types.FetchResponse, nil
	default:
		return types.NullMessage, fmt.Errorf("Unmappable message type")
	}
}
