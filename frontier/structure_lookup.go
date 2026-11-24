package frontier

import (
	"fmt"
	"strings"

	"github.com/dgraph-io/badger/v3"
)

// Both evidence policies use the same bounded structural lookup. Their
// observations, thresholds and decisions remain independent.
func structureCandidates(tx *badger.Txn, prefix string, signature []uint64) (map[string]bool, error) {
	out := map[string]bool{}
	if len(signature) != 64 {
		return out, nil
	}
	for band := 0; band < 16; band++ {
		p := fmt.Sprintf("%s%02d/%016x%016x%016x%016x/", prefix, band, signature[band*4], signature[band*4+1], signature[band*4+2], signature[band*4+3])
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := tx.NewIterator(opts)
		for it.Seek([]byte(p)); it.ValidForPrefix([]byte(p)) && len(out) < 64; it.Next() {
			out[strings.TrimPrefix(string(it.Item().Key()), p)] = true
		}
		it.Close()
	}
	return out, nil
}
func indexStructure(tx *badger.Txn, prefix, id string, signature []uint64) error {
	for band := 0; band < 16; band++ {
		k := fmt.Sprintf("%s%02d/%016x%016x%016x%016x/%s", prefix, band, signature[band*4], signature[band*4+1], signature[band*4+2], signature[band*4+3], id)
		if err := write(tx, k, true); err != nil {
			return err
		}
	}
	return nil
}
