package protection

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/site"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// SanitizeJSON removes matching records and references without converting large
// JSON integers to floating point. A nil result means the entire record matched.
func SanitizeJSON(raw []byte, match func(string) bool) ([]byte, bool, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value interface{}
	if e := dec.Decode(&value); e != nil {
		return nil, false, e
	}
	changed := false
	var walk func(interface{}) interface{}
	walk = func(v interface{}) interface{} {
		switch x := v.(type) {
		case string:
			if References(x, match) || !strings.ContainsAny(x, " /\t\r\n") && (strings.Contains(x, ".") || net.ParseIP(x) != nil) && match(site.HTTPURL(x)) {
				changed = true
				return nil
			}
		case []interface{}:
			out := []interface{}{}
			for _, c := range x {
				if n := walk(c); n != nil {
					out = append(out, n)
				}
			}
			return out
		case map[string]interface{}:
			for _, key := range []string{"uri", "effective_uri"} {
				if u, ok := x[key].(string); ok && match(u) {
					changed = true
					return nil
				}
			}
			for k, c := range x {
				if match(k) || (strings.Contains(k, ".") || net.ParseIP(k) != nil) && match(site.HTTPURL(k)) {
					delete(x, k)
					changed = true
					continue
				}
				n := walk(c)
				if n == nil {
					delete(x, k)
				} else {
					x[k] = n
				}
			}
			if _, ok := x["message_type"]; ok && x["message"] == nil {
				changed = true
				return nil
			}
		}
		return v
	}
	value = walk(value)
	if !changed {
		return raw, false, nil
	}
	if value == nil {
		return nil, true, nil
	}
	out, e := json.Marshal(value)
	return out, true, e
}
func RewriteJSONL(ctx context.Context, path string, match func(string) bool) error {
	if info, e := os.Lstat(path); e == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("results destination is not a regular file")
	}
	if e := cleanupRewriteTemps(ctx, path); e != nil {
		return e
	}
	in, e := os.Open(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	defer in.Close()
	info, statErr := in.Stat()
	if statErr != nil {
		return statErr
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("results destination is not a regular file")
	}
	out, e := os.CreateTemp(filepath.Dir(path), rewriteTempPattern(path))
	if e != nil {
		return e
	}
	defer os.Remove(out.Name())
	defer out.Close()
	if e = out.Chmod(0600); e != nil {
		return e
	}
	r := bufio.NewReaderSize(in, 64<<10)
	changed := false
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		line, readErr := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			next, diff, e := SanitizeJSON(line, match)
			if e != nil {
				return fmt.Errorf("purge results: %w", e)
			}
			changed = changed || diff
			if next != nil {
				if _, e = out.Write(append(bytes.TrimSpace(next), '\n')); e != nil {
					return e
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if !changed {
		return nil
	}
	if e = out.Sync(); e != nil {
		return e
	}
	if e = out.Close(); e != nil {
		return e
	}
	if e = os.Rename(out.Name(), path); e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}

func RewriteJSONFile(ctx context.Context, path string, match func(string) bool) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := cleanupRewriteTemps(ctx, path); e != nil {
		return e
	}
	raw, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if !json.Valid(raw) {
		return nil
	}
	clean, changed, e := SanitizeJSON(raw, match)
	if e != nil || !changed {
		return e
	}
	if clean == nil {
		if e := os.Remove(path); e != nil {
			return e
		}
		return SyncDirectory(filepath.Dir(path))
	}
	f, e := os.CreateTemp(filepath.Dir(path), rewriteTempPattern(path))
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = f.Chmod(0600); e != nil {
		return e
	}
	if _, e = f.Write(clean); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e := os.Rename(f.Name(), path); e != nil {
		return e
	}
	return SyncDirectory(filepath.Dir(path))
}
func VerifyJSONL(ctx context.Context, path string, match func(string) bool) error {
	if info, e := os.Lstat(path); e == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("results destination is not a regular file")
	}
	if e := verifyRewriteTemps(ctx, path); e != nil {
		return e
	}
	in, e := os.Open(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	defer in.Close()
	info, statErr := in.Stat()
	if statErr != nil {
		return statErr
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("results destination is not a regular file")
	}
	r := bufio.NewReaderSize(in, 64<<10)
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		raw, readErr := r.ReadBytes('\n')
		if len(bytes.TrimSpace(raw)) > 0 {
			_, changed, e := SanitizeJSON(raw, match)
			if e != nil {
				return e
			}
			if changed {
				return fmt.Errorf("matching data remains in %s", filepath.Base(path))
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// SyncDirectory makes a completed unlink or rename durable across a restart.
func SyncDirectory(path string) error {
	d, e := os.Open(path)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}

func ReplaceFile(ctx context.Context, path string, raw []byte) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := cleanupRewriteTemps(ctx, path); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), rewriteTempPattern(path))
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = f.Chmod(0600); e != nil {
		return e
	}
	if _, e = f.Write(raw); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	return SyncDirectory(filepath.Dir(path))
}
