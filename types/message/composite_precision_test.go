package message

import (
	"encoding/json"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types/features"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCompositeAnalysisPreservesStructuralFingerprintBits(t *testing.T) {
	signature := make([]uint64, 64)
	for i := range signature {
		signature[i] = ^uint64(0) - uint64(i)*901
	}
	original := CompositeAnalysis{Features: map[string]interface{}{features.AdversarialField: features.Adversarial{Structure: &trap.Observation{Signature: signature, Targets: signature[:10], Patterns: signature[:8]}}}}
	raw, e := json.Marshal(original)
	require.NoError(t, e)
	for relay := 0; relay < 3; relay++ {
		var decoded CompositeAnalysis
		require.NoError(t, json.Unmarshal(raw, &decoded))
		var structure features.Adversarial
		require.NoError(t, decoded.Load(features.AdversarialField, &structure))
		require.Equal(t, signature, []uint64(structure.Structure.Signature), "JSON relay changed fingerprint bits")
		require.Equal(t, signature[:10], []uint64(structure.Structure.Targets))
		require.Equal(t, signature[:8], []uint64(structure.Structure.Patterns))
		raw, e = json.Marshal(decoded)
		require.NoError(t, e)
	}
}
