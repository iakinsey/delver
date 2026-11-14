package queue

import (
	"encoding/json"
	"github.com/iakinsey/delver/types"
)

func objectReferences(data []byte) ([]types.UUID, error) {
	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	var result []types.UUID
	var walk func(interface{})
	walk = func(value interface{}) {
		switch v := value.(type) {
		case []interface{}:
			for _, child := range v {
				walk(child)
			}
		case map[string]interface{}:
			if kind, ok := v["message_type"].(float64); ok && types.MessageType(kind) == types.FetcherResponseType {
				if message, ok := v["message"].(map[string]interface{}); ok {
					if key, ok := message["store_key"].(string); ok && key != "" {
						result = append(result, types.UUID(key))
					}
				}
			}
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(value)
	return result, nil
}
