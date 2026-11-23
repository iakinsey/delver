package fetcher

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/iakinsey/delver/protection"
)

var errMappedBodyUnavailable = errors.New("mapped response staging unavailable")

type mappedBody struct {
	file    *os.File
	data    []byte
	used    int
	release func()
}

func (b *mappedBody) Close() {
	if b == nil || b.release == nil {
		return
	}
	clear(b.data[:b.used])
	b.release()
	b.release = nil
	b.data = nil
}

func textualResponse(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return strings.Contains(contentType, "text/") || strings.Contains(contentType, "html") || strings.Contains(contentType, "xml") || strings.Contains(contentType, "json") || contentType == ""
}

func (s *httpFetcher) readMappedProtected(ctx context.Context, original, effective string, source io.Reader, size int64, contentType string) (body *mappedBody, err error) {
	if effective == "" {
		effective = original
	}
	if err := s.protection.Check(effective); err != nil {
		return nil, err
	}
	create := s.createMappedBody
	if create == nil {
		create = newMappedBody
	}
	body, err = create(size)
	if err != nil {
		body.Close()
		return nil, errMappedBodyUnavailable
	}
	owned := body
	defer func() {
		if err != nil {
			owned.Close()
			body = nil
		}
	}()
	// Mapped bodies have an exact bounded size. Keep a conservative 8 MiB
	// allowance for overlapping windows, Unicode expansion and normalization
	// temporaries, while never reserving more than the legacy configured bound.
	limit := s.maxBytes
	if limit <= 0 {
		limit = 16 << 20
	}
	releaseMemory, reserveErr := s.reserveInspection(ctx, min(2*limit+(64<<10), 2*size+(8<<20)))
	if reserveErr != nil {
		return nil, reserveErr
	}
	releaseBody := body.release
	body.release = func() { releaseBody(); releaseMemory() }
	var inspector *protection.Inspector
	if textualResponse(contentType) {
		inspector = s.protection.InspectReader(ctx, nil, effective, s.crawlID, s.runID, s.purgeDepth)
		defer inspector.Close()
	}
	emptyReads := 0
	for body.used < len(body.data) {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		n, readErr := source.Read(body.data[body.used:min(body.used+(32<<10), len(body.data))])
		part := body.data[body.used : body.used+n]
		body.used += n
		if n > 0 {
			emptyReads = 0
			if inspector != nil {
				if err = inspector.InspectChunk(part); err != nil {
					return nil, err
				}
			} else if err = s.protection.Check(effective); err != nil {
				return nil, err
			}
		} else {
			emptyReads++
		}
		if readErr != nil {
			if readErr == io.EOF && body.used == len(body.data) {
				return body, nil
			}
			if readErr == io.EOF {
				readErr = io.ErrUnexpectedEOF
			}
			return nil, readErr
		}
		if emptyReads >= 100 {
			return nil, io.ErrNoProgress
		}
	}
	// Reading EOF releases the normal HTTP transport's permits and validates
	// framing. No raw socket access, alternate transport, or second GET is used.
	var probe [1]byte
	for attempts := 0; attempts < 100; attempts++ {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		n, readErr := source.Read(probe[:])
		if n != 0 {
			return nil, errors.New("response content length mismatch")
		}
		if readErr == io.EOF {
			return body, nil
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return nil, io.ErrNoProgress
}
