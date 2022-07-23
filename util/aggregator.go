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
	aggFn             func([]float64) float64
	timeField         string
	aggField          string
	timeWindowSeconds float64
	nextTime          *float64
	timeWindow        []float64
	valueWindow       []float64
}

func getAggFn(name string) (func([]float64) float64, error) {
	switch name {
	case "sum":
		return Sum, nil
	case "mean":
		return Mean, nil
	case "median":
		return Median, nil
	case "mode":
		return Mode, nil
	}

	return nil, fmt.Errorf("no such agg function: %s", name)
}

func NewAggregator(name string, timeField string, aggField string, timeWindowSeconds float64) (Aggregator, error) {
	aggFn, err := getAggFn(name)

	if err != nil {
		return nil, err
	}

	if timeField == "" || aggField == "" {
		return nil, errors.New("time field and agg field cannot be empty")
	}

	if timeWindowSeconds <= 0 {
		return nil, errors.New("time window cannot be less than or equal to 0")
	}

	return &aggregator{
		aggFn:             aggFn,
		timeField:         timeField,
		aggField:          aggField,
		timeWindowSeconds: timeWindowSeconds,
		timeWindow:        make([]float64, 0),
		valueWindow:       make([]float64, 0),
	}, nil
}

func (s *aggregator) Perform(entity map[string]float64) map[string]float64 {
	time, ok := entity[s.timeField]

	if !ok {
		return nil
	}

	val, ok := entity[s.aggField]

	if !ok {
		return nil
	}

	if s.nextTime == nil {
		s.reset(time)
	}

	if time < *s.nextTime {
		s.timeWindow = append(s.timeWindow, time)
		s.valueWindow = append(s.valueWindow, val)

		return nil
	} else if len(s.timeWindow) == 0 {
		return nil
	}

	next := map[string]float64{
		s.aggField:  s.aggFn(s.valueWindow),
		s.timeField: Median(s.timeWindow),
	}

	s.reset(time)

	return next
}

func (s *aggregator) reset(time float64) {
	s.timeWindow = make([]float64, 0)
	s.valueWindow = make([]float64, 0)
	s.nextTime = &time
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
func Mode(l []float64) (r float64) {
	if len(l) == 1 {
		return l[0]
	}

	m := make(map[float64]int, 0)
	var maxK float64
	var maxV int

	for _, n := range l {
		m[n] += 1

		if m[n] > maxV {
			maxK = n
			maxV = m[n]
		}
	}

	return maxK
}
