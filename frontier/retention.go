package frontier

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// Lifetime is either a positive duration or "never". Zero means no expiration.
type Lifetime time.Duration

func (d *Lifetime) UnmarshalJSON(b []byte) error {
	var value string
	if err := json.Unmarshal(b, &value); err != nil {
		return err
	}
	if value == "never" {
		*d = 0
		return nil
	}
	parseValue := value
	days := strings.HasSuffix(value, "d")
	if days {
		parseValue = strings.TrimSuffix(value, "d") + "h"
	}
	duration, err := time.ParseDuration(parseValue)
	if days {
		if duration <= 0 {
			return fmt.Errorf("retention must be positive")
		}
		if duration > time.Duration(math.MaxInt64/24) {
			return fmt.Errorf("retention duration overflows")
		}
		duration *= 24
	}
	if err != nil || duration <= 0 {
		return fmt.Errorf("retention must be a positive duration or never: %q", value)
	}
	*d = Lifetime(duration)
	return nil
}
func (d Lifetime) MarshalJSON() ([]byte, error) {
	if d == 0 {
		return json.Marshal("never")
	}
	return json.Marshal(time.Duration(d).String())
}

type Retention struct {
	CompletedFor     Lifetime `json:"completed_for"`
	FailedFor        Lifetime `json:"failed_for"`
	DedupFor         Lifetime `json:"dedup_for"`
	CleanupInterval  Lifetime `json:"cleanup_interval"`
	CleanupBatchSize int      `json:"cleanup_batch_size"`
}

func DefaultRetention() Retention {
	return Retention{Lifetime(7 * 24 * time.Hour), Lifetime(30 * 24 * time.Hour), Lifetime(90 * 24 * time.Hour), Lifetime(5 * time.Minute), 1000}
}
func (r *Retention) UnmarshalJSON(b []byte) error {
	type plain Retention
	value := plain(DefaultRetention())
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*r = Retention(value)
	return r.Validate()
}
func (r Retention) Validate() error {
	if r.CleanupInterval <= 0 || r.CleanupBatchSize < 1 {
		return fmt.Errorf("cleanup interval and batch size must be positive")
	}
	for _, d := range []Lifetime{r.CompletedFor, r.FailedFor} {
		if d < 0 || r.DedupFor < 0 || (r.DedupFor != 0 && (d == 0 || r.DedupFor < d)) {
			return fmt.Errorf("dedup retention must cover detail retention")
		}
	}
	return nil
}
