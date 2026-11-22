package trap

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
)

// HashVector keeps bounded 64-bit hashes exact through JSON relays and search sinks.
// The versioned binary representation also avoids duplicating decimal vectors.
type HashVector []uint64

func (s HashVector) MarshalJSON() ([]byte, error) {
	if len(s) == 0 {
		return []byte("null"), nil
	}
	if len(s) > 128 {
		return nil, fmt.Errorf("structural hash vector exceeds 128 hashes")
	}
	raw := make([]byte, len(s)*8)
	for i, value := range s {
		binary.LittleEndian.PutUint64(raw[i*8:], value)
	}
	return json.Marshal("v1:" + base64.StdEncoding.EncodeToString(raw))
}
func (s *HashVector) UnmarshalJSON(raw []byte) error {
	if len(raw) > 8192 {
		return fmt.Errorf("structural hash vector exceeds wire limit")
	}
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		*s = nil
		return nil
	}
	if len(raw) > 0 && raw[0] == '[' {
		var legacy []uint64
		if e := json.Unmarshal(raw, &legacy); e != nil {
			return e
		}
		if len(legacy) > 128 {
			return fmt.Errorf("structural hash vector exceeds 128 hashes")
		}
		*s = legacy
		return nil
	}
	var encoded string
	if e := json.Unmarshal(raw, &encoded); e != nil {
		return e
	}
	if !strings.HasPrefix(encoded, "v1:") {
		return fmt.Errorf("unsupported structural sketch encoding")
	}
	value, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "v1:"))
	if e != nil {
		return e
	}
	if len(value) > 128*8 || len(value)%8 != 0 {
		return fmt.Errorf("invalid structural hash vector length")
	}
	hashes := make(HashVector, len(value)/8)
	for i := range hashes {
		hashes[i] = binary.LittleEndian.Uint64(value[i*8:])
	}
	*s = hashes
	return nil
}
