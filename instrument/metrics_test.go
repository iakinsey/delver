package instrument

import (
	"encoding/json"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"testing"
	"time"
)

func TestIdleMetricsAreExported(t *testing.T) {
	q := queue.NewChannelQueue()
	defer q.Stop()
	sink := LoadMetrics(q)
	defer Stop()
	sink.IncrCounter([]string{"idle", "request"}, 1)
	deadline := time.After(3 * time.Second)
	for {
		select {
		case message := <-q.GetChannel():
			var points []types.Metric
			if err := json.Unmarshal(message.Message, &points); err != nil {
				t.Fatal(err)
			}
			for _, point := range points {
				if point.Key == "idle.request" && point.Value == 1 {
					return
				}
			}
		case <-deadline:
			t.Fatal("last interval was not exported without another metric event")
		}
	}
}
