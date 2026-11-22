package application

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestProtectionRemovesOrphanAndInterruptedAtlasSnapshots(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "run.report.sqlite")
	removed := []string{".atlas.sqlite", ".atlas.sqlite-wal", ".atlas.sqlite-shm", ".atlas.sqlite.job.tmp", ".atlas.sqlite.job.tmp-journal"}
	retained := []string{".other", ".atlas.sqlite.other", ".atlas.sqlite-other.tmp"}
	for _, suffix := range append(removed, retained...) {
		require.NoError(t, os.WriteFile(report+suffix, []byte("fixture"), 0600))
	}
	require.NoError(t, removeProtectionAtlasArtifacts(report))
	for _, suffix := range removed {
		_, e := os.Stat(report + suffix)
		require.True(t, os.IsNotExist(e), suffix)
	}
	for _, suffix := range retained {
		_, e := os.Stat(report + suffix)
		require.NoError(t, e, suffix)
	}
	require.NoError(t, removeProtectionAtlasArtifacts(report))
}
