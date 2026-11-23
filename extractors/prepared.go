package extractors

import (
	"context"
	"sync"

	"github.com/iakinsey/delver/trap"
)

type preparedKey struct{}
type preparedAnalysis struct {
	sync.Mutex
	structure *trap.Structure
	uri       string
}

// WithPreparedAnalysis scopes optional built-in work reuse to one delivery.
// Custom extractors need not participate and keep their file-based behavior.
func WithPreparedAnalysis(ctx context.Context) context.Context {
	return context.WithValue(ctx, preparedKey{}, &preparedAnalysis{})
}

func WithPreparedAnalysisFor(ctx context.Context, enabled []Extractor) context.Context {
	urls, structure := false, false
	for _, ext := range enabled {
		switch ext.(type) {
		case *urlExtractor:
			urls = true
		case *adversarialExtractor:
			structure = true
		}
	}
	if urls && structure {
		return WithPreparedAnalysis(ctx)
	}
	return ctx
}

func prepared(ctx context.Context) *preparedAnalysis {
	p, _ := ctx.Value(preparedKey{}).(*preparedAnalysis)
	return p
}
