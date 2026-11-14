package message

import (
	"encoding/json"
	"fmt"
)

type CompositeAnalysis struct {
	FetcherResponse

	ExtractorErrors map[string]string      `json:"extractor_errors,omitempty"`
	Features        map[string]interface{} `json:"features"`
}

func (s *CompositeAnalysis) Has(key string) bool {
	_, ok := s.Features[key]

	return ok
}

func (s *CompositeAnalysis) Load(key string, val interface{}) error {
	feature, ok := s.Features[key]
	if !ok {
		return fmt.Errorf("feature of key %s does not exist", key)
	}
	data, err := json.Marshal(feature)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, val)
}
func (s *CompositeAnalysis) LoadPermissive(key string, val interface{}) bool {
	return s.Load(key, val) == nil
}
