package util

import (
	"fmt"
	"math"
	"sort"

	"github.com/pkg/errors"
)

type Aggregator interface {
	Perform(map[string]float64) map[string]float64
}

type aggregator struct {
	name                string
	timeField, aggField string
	window              float64
	start               *float64
	sum, count          float64
}

func NewAggregator(name, timeField, aggField string, window int64) (Aggregator, error) {
	if name == "" {
		name = "sum"
	}
	if name != "sum" && name != "avg" {
		return nil, fmt.Errorf("no such agg function: %s", name)
	}
	if timeField == "" || aggField == "" || window <= 0 {
		return nil, errors.New("aggregation requires fields and a positive window")
	}
	return &aggregator{name: name, timeField: timeField, aggField: aggField, window: float64(window)}, nil
}
func (s *aggregator) Perform(entity map[string]float64) map[string]float64 {
	timestamp, ok := entity[s.timeField]
	if !ok {
		return nil
	}
	value, ok := entity[s.aggField]
	if !ok {
		return nil
	}
	start := math.Floor(timestamp/s.window) * s.window
	if s.start != nil && start < *s.start {
		return nil
	}
	var result map[string]float64
	if s.start == nil || start > *s.start {
		result = s.Snapshot()
		s.start = &start
		s.sum = 0
		s.count = 0
	}
	s.sum += value
	s.count++
	return result
}
func (s *aggregator) Snapshot() map[string]float64 {
	if s.start == nil || s.count == 0 {
		return nil
	}
	value := s.sum
	if s.name == "avg" {
		value /= s.count
	}
	return map[string]float64{s.timeField: *s.start, s.aggField: value}
}
func (s *aggregator) Seed(timestamp, value, count float64) {
	start := math.Floor(timestamp/s.window) * s.window
	s.start = &start
	s.count = count
	s.sum = value
	if s.name == "avg" {
		s.sum = value * count
	}
}
func (s *aggregator) Flush() map[string]float64 {
	result := s.Snapshot()
	s.start = nil
	s.sum = 0
	s.count = 0
	return result
}

// Agg functions
func Sum(l []float64) (r float64) {
	for _, n := range l {
		r += n
	}
	return
}

func Mean(l []float64) float64 {
	var s float64
	var c float64

	for _, n := range l {
		s += n
		c += 1
	}

	return s / c
}
func Median(l []float64) (r float64) {
	if len(l) == 1 {
		return l[0]
	}

	sort.Float64s(l)

	return l[int(math.Floor(float64(len(l))/2.0))]
}
func Min(l []float64) (r float64) {
	if len(l) == 1 {
		return l[0]
	}

	sort.Float64s(l)

	return l[0]
}
