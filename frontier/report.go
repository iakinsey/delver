package frontier

import (
	"context"
	"encoding/json"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/report"
	"github.com/iakinsey/delver/types/message"
)

// ReportPage reads bounded retained URL and staged-discovery records without
// changing scheduling state. The cursor remains valid when records disappear.
func (s *Store) ReportPage(ctx context.Context, cursor string, limit int) ([]report.URL, string, error) {
	return reportPage(ctx, s.db, cursor, limit)
}
func ReadReportPage(ctx context.Context, path, cursor string, limit int) ([]report.URL, string, error) {
	opts := badger.DefaultOptions(path).WithReadOnly(true).WithLogger(nil)
	db, e := badger.Open(opts)
	if e != nil {
		return nil, "", e
	}
	defer db.Close()
	return reportPage(ctx, db, cursor, limit)
}
func reportPage(ctx context.Context, db *badger.DB, cursor string, limit int) ([]report.URL, string, error) {
	out := []report.URL{}
	next := ""
	e := db.View(func(tx *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := tx.NewIterator(opts)
		defer it.Close()
		for _, prefix := range []string{"staged/", "u/"} {
			if cursor != "" && cursor > prefix+"\xff" {
				continue
			}
			seek := prefix
			if cursor >= prefix {
				seek = cursor + "\x00"
			}
			for it.Seek([]byte(seek)); it.ValidForPrefix([]byte(prefix)); it.Next() {
				if e := ctx.Err(); e != nil {
					return e
				}
				if len(out) >= limit {
					return nil
				}
				key := string(it.Item().Key())
				var req message.FetcherRequest
				if e := it.Item().Value(func(raw []byte) error {
					if prefix == "u/" {
						var c Candidate
						if e := json.Unmarshal(raw, &c); e != nil {
							return e
						}
						req = c.Request
						return nil
					}
					return json.Unmarshal(raw, &req)
				}); e != nil {
					return e
				}
				out = append(out, report.URL{URI: req.URI, Origin: req.Origin})
				next = key
			}
		}
		next = ""
		return nil
	})
	return out, next, e
}
