package message

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
)

type CompositeAnalysis struct {
	FetcherResponse

	Features map[string]interface{} `json:"features"`
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

	s.setFeature(key, val, feature)

	return nil
}

func (s *CompositeAnalysis) LoadPermissive(key string, val interface{}) bool {
	feature, ok := s.Features[key]

	if !ok {
		return false
	}

	s.setFeature(key, val, feature)

	return true
}

func (s *CompositeAnalysis) setFeature(key string, val interface{}, feature interface{}) {
	rv := reflect.ValueOf(val)

	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		log.Fatalf("value for key %s is not a pointer", key)
	}

	f := reflect.New(reflect.TypeOf(feature))
	fElem := f.Elem()

	f.Elem().Set(reflect.ValueOf(feature))

	s.setElem(rv, fElem, key, feature, val)
}

func (s *CompositeAnalysis) setElem(rv reflect.Value, fElem reflect.Value, key string, feature interface{}, val interface{}) {
	defer func() {
		if r := recover(); r == nil {
			return
		}

		b, err := json.Marshal(feature)

		if err != nil {
			panic(err)
		}

		if err := json.Unmarshal(b, val); err != nil {
			panic(err)
		}

		s.Features[key] = val
	}()

	rv.Elem().Set(fElem)
}
