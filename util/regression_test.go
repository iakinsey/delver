package util

import (
	"reflect"
	"testing"
)

func TestRegressionWindowAggregation(t *testing.T) {
	a, _ := NewAggregator("sum", "when", "value", 60)
	inputs := []map[string]float64{{"when": 100, "value": 2}, {"when": 110, "value": 3}, {"when": 161, "value": 5}}
	for i, e := range inputs {
		r := a.Perform(e)
		if i < 2 && r != nil {
			t.Fatalf("60-second window emitted after %d samples: %v", i+1, r)
		}
		if i == 2 && !reflect.DeepEqual(r, map[string]float64{"when": 60, "value": 5}) {
			t.Fatalf("window sum: %v", r)
		}
	}
}
func TestRegressionFlattenMetric(t *testing.T) {
	result := map[string]float64{}
	if err := FlattenJSON(map[string]interface{}{"key": "fetch", "when": float64(100), "value": float64(2)}, "", result); err != nil {
		t.Fatalf("ordinary metric cannot be flattened: %v", err)
	}
}
