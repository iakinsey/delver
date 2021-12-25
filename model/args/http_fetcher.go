package args

import (
	"time"

	"github.com/iakinsey/delver/gateway/streamstore"
)

type HTTPFetcherArgs struct {
	UserAgent   string
	MaxRetries  int
	Timeout     time.Duration
	ProxyHost   string
	ProxyPort   string
	StreamStore streamstore.StreamStore
}
