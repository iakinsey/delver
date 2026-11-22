package trap

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestHashVectorsRemainExactAndReadLegacyArrays(t *testing.T) {
	for _, size := range []int{0, 1, 64, 128} {
		var original HashVector
		if size > 0 {
			original = make(HashVector, size)
		}
		for i := range original {
			original[i] = ^uint64(0) - uint64(i)
		}
		encoded, e := json.Marshal(original)
		require.NoError(t, e)
		var decoded HashVector
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		require.EqualValues(t, original, decoded)
		legacy, e := json.Marshal([]uint64(original))
		require.NoError(t, e)
		require.NoError(t, json.Unmarshal(legacy, &decoded))
		require.EqualValues(t, original, decoded)
	}
}
func TestHashVectorRejectsMalformedOrUnboundedValues(t *testing.T) {
	for _, raw := range []string{`"v2:AAAA"`, `"v1:AAAA"`, `"v1:invalid!"`, strings.Repeat(" ", 8193), `{}`, `[18446744073709551616]`} {
		var v HashVector
		require.Error(t, json.Unmarshal([]byte(raw), &v), raw)
	}
	_, e := json.Marshal(make(HashVector, 129))
	require.Error(t, e)
	raw, e := json.Marshal(make([]uint64, 129))
	require.NoError(t, e)
	var v HashVector
	require.Error(t, json.Unmarshal(raw, &v))
}
