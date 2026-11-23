package protection

import (
	"context"
	"io"
)

// Inspector scans bounded text windows while a normal response consumer reads.
// It owns no stored body and closes only its own inspection buffer.
type Inspector struct {
	ctx             context.Context
	source          io.Reader
	policy          *Policy
	uri, crawl, run string
	depth           int
	window          []byte
}

func (p *Policy) InspectReader(ctx context.Context, source io.Reader, uri, crawl, run string, depth int) *Inspector {
	return &Inspector{ctx: ctx, source: source, policy: p, uri: uri, crawl: crawl, run: run, depth: depth, window: make([]byte, 0, 64<<10)}
}
func (r *Inspector) Read(buffer []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	if e := r.policy.Check(r.uri); e != nil {
		return 0, e
	}
	n, e := r.source.Read(buffer)
	if failure := r.InspectChunk(buffer[:n]); failure != nil {
		clear(buffer[:n])
		return 0, failure
	}
	return n, e
}

// InspectChunk applies the same incremental inspection to a caller-owned buffer.
// It does not retain that buffer; only the bounded inspection window is kept.
func (r *Inspector) InspectChunk(buffer []byte) error {
	if e := r.ctx.Err(); e != nil {
		return e
	}
	if e := r.policy.Check(r.uri); e != nil {
		return e
	}
	for offset := 0; offset < len(buffer); {
		end := min(len(buffer), offset+(32<<10))
		part := buffer[offset:end]
		if len(r.window)+len(part) > 64<<10 {
			keep := (64 << 10) - len(part)
			copy(r.window, r.window[len(r.window)-keep:])
			r.window = r.window[:keep]
		}
		r.window = append(r.window, part...)
		if failure := r.policy.Inspect(r.uri, r.window, r.crawl, r.run, r.depth); failure != nil {
			r.Close()
			return failure
		}
		offset = end
	}
	return nil
}
func (r *Inspector) Close() error { clear(r.window); r.window = nil; return nil }
