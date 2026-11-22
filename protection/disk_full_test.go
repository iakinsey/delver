package protection

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Run on a small, private tmpfs, e.g. Docker --tmpfs /fault:size=2m.
func TestProtectionRealDiskFullRecovery(t *testing.T) {
	parent := os.Getenv("DELVER_PROTECTION_FULL_DIR")
	if parent == "" {
		t.Skip("set DELVER_PROTECTION_FULL_DIR to a private, size-limited filesystem")
	}
	var stat syscall.Statfs_t
	if e := syscall.Statfs(parent, &stat); e != nil {
		t.Fatal(e)
	}
	if stat.Blocks*uint64(stat.Bsize) > 16<<20 {
		t.Fatal("disk-full fixture must be isolated and no larger than 16 MiB")
	}
	root, e := os.MkdirTemp(parent, "delver-disk-full-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(root)
	p, e := Open(filepath.Join(root, "policy.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	filler := filepath.Join(root, "filler")
	file, e := os.Create(filler)
	if e != nil {
		t.Fatal(e)
	}
	chunk := make([]byte, 32<<10)
	for {
		_, e = file.Write(chunk)
		if e != nil {
			break
		}
	}
	file.Close()
	if !errors.Is(e, syscall.ENOSPC) {
		t.Fatal("fixture did not exhaust storage", e)
	}
	_, e = p.Trigger("http://synthetic.test/", "synthetic", "crawl", "run", 0)
	if e == nil || errors.Is(e, ErrBlocked) {
		t.Fatal("undurable detection was acknowledged", e)
	}
	if !errors.Is(p.Check("http://unrelated.test/"), ErrUnavailable) {
		t.Fatal("admission remained open on disk failure")
	}
	if e = os.Remove(filler); e != nil {
		t.Fatal(e)
	}
	if e = p.Flush(); e != nil {
		t.Fatal(e)
	}
	if !errors.Is(p.Check("http://synthetic.test/"), ErrBlocked) || p.Check("http://unrelated.test/") != nil {
		t.Fatal("deny/recovery incorrect")
	}
	public := p.PublicSnapshot()
	if len(public.Incidents) != 1 || public.Incidents[0].State == "completed" {
		t.Fatal("incomplete purge marked complete")
	}
	if e = p.Close(); e != nil {
		t.Fatal(e)
	}
	p, e = Open(filepath.Join(root, "policy.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if !errors.Is(p.Check("http://synthetic.test/"), ErrBlocked) {
		t.Fatal("durable deny lost after restart")
	}
	if strings.Contains(p.Health(), "unavailable") {
		t.Fatal("disk recovery retained stale error")
	}
}
