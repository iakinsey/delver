package repetition

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestDefaultsCalibrationAndPartialConfiguration(t *testing.T) {
	raw, err := os.ReadFile("testdata/defaults.json")
	require.NoError(t, err)
	var settings Settings
	require.NoError(t, json.Unmarshal(raw, &settings))
	require.Equal(t, Default(), settings)
	require.NoError(t, json.Unmarshal([]byte(`{"mode":"off"}`), &settings))
	want := Default()
	want.Mode = "off"
	require.Equal(t, want, settings)
	for _, raw := range []string{`{"mode":"other"}`, `{"mode":"observe","unknown":1}`, `{"minimum_sites":2}`, `{"admission_share_percent":0}`, `{"site_window":101}`, `{"host_dominance":1.1}`} {
		require.Error(t, json.Unmarshal([]byte(raw), &settings))
	}
}
func TestTurnShares(t *testing.T) {
	for share := 1; share <= 50; share++ {
		n := 0
		for step := uint64(0); step < 100; step++ {
			if Turn(step, share) {
				n++
			}
		}
		require.Equal(t, share, n)
	}
}
