package protection

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Callers serialize rewrites of a destination (the sink or workspace lock).
// Destination-specific names let recovery distinguish its abandoned copies
// from a concurrent rewrite of another file in the same directory.
func rewriteTempPattern(path string) string {
	return fmt.Sprintf(".purge-%x-*", sha256.Sum256([]byte(filepath.Base(path))))
}

func visitRewriteTemps(ctx context.Context, path string, visit func(string) error) error {
	dir := filepath.Dir(path)
	f, err := os.Open(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := strings.TrimSuffix(rewriteTempPattern(path), "*")
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := f.ReadDir(128)
		if err != nil && err != io.EOF {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			// Earlier, unreleased rewrites used only a random decimal suffix.
			legacy := strings.TrimPrefix(name, ".purge-")
			old := legacy != name && legacy != "" && strings.Trim(legacy, "0123456789") == ""
			if !strings.HasPrefix(name, prefix) && !old {
				continue
			}
			info, e := entry.Info()
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("unsupported temporary rewrite entry")
			}
			if e := visit(filepath.Join(dir, name)); e != nil {
				return e
			}
		}
		if err == io.EOF {
			return nil
		}
	}
}

func cleanupRewriteTemps(ctx context.Context, path string) error {
	removed := false
	if err := visitRewriteTemps(ctx, path, func(name string) error {
		if err := os.Remove(name); err != nil {
			return err
		}
		removed = true
		return nil
	}); err != nil {
		return err
	}
	if removed {
		return SyncDirectory(filepath.Dir(path))
	}
	return nil
}

func verifyRewriteTemps(ctx context.Context, path string) error {
	return visitRewriteTemps(ctx, path, func(string) error {
		return fmt.Errorf("temporary rewrite copy remains")
	})
}
