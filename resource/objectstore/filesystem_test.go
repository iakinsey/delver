package objectstore

import (
	"errors"
	"github.com/iakinsey/delver/types"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("interrupted body") }
func TestAtomicBodyWriteAndReconciliation(t *testing.T) {
	root := t.TempDir()
	store := NewFilesystemObjectStore(FilesystemObjectStoreParams{Path: root}).(*filesystemObjectStore)
	key := types.NewV4()
	if _, err := store.Put(key, brokenReader{}); err == nil {
		t.Fatal("partial write accepted")
	}
	files, _ := os.ReadDir(root)
	if len(files) != 0 {
		t.Fatal("partial file leaked")
	}
	kept, orphan := types.NewV4(), types.NewV4()
	store.Put(kept, strings.NewReader("keep"))
	store.Put(orphan, strings.NewReader("orphan"))
	if err := store.Reconcile(map[types.UUID]bool{kept: true}); err != nil {
		t.Fatal(err)
	}
	f, err := store.Get(kept)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(f)
	f.Close()
	if string(data) != "keep" {
		t.Fatal("retained body changed")
	}
	if _, err := os.Stat(filepath.Join(root, string(orphan))); !os.IsNotExist(err) {
		t.Fatal("orphan retained")
	}
	if _, err := store.Get(""); err == nil {
		t.Fatal("empty key accepted")
	}
}
