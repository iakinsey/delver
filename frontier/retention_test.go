package frontier

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRetention(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{`{}`, true}, {`{"completed_for":"7d"}`, true}, {`{"dedup_for":"never"}`, true}, {`{"completed_for":"never","failed_for":"never","dedup_for":"never"}`, true},
		{`{"dedup_for":"1d"}`, false}, {`{"completed_for":"never"}`, false}, {`{"completed_for":"0s"}`, false}, {`{"cleanup_interval":"never"}`, false},
	} {
		var r Retention
		err := json.Unmarshal([]byte(tc.value), &r)
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.value, err)
		}
	}
	var r Retention
	if err := json.Unmarshal([]byte(`{"dedup_for":"never"}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.CompletedFor != Lifetime(7*24*time.Hour) || r.DedupFor != 0 {
		t.Fatal(r)
	}
}
