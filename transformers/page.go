package transformers

import (
	"crypto/md5"
	"encoding/json"
	"fmt"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/pkg/errors"
)

type pageTransformer struct{}

func NewPageTransformer() Transformer {
	return &pageTransformer{}
}

func (s *pageTransformer) Perform(msg json.RawMessage) ([]*types.Indexable, error) {
	composite := message.CompositeAnalysis{}

	if err := json.Unmarshal(msg, &composite); err != nil {
		return nil, errors.Wrap(err, "transformer failed to parse article")
	}

	page := types.Page{
		Uri:           composite.URI,
		Host:          composite.Host,
		Origin:        composite.Origin,
		Protocol:      string(composite.Protocol),
		ContentMd5:    composite.ContentMD5,
		ElapsedTimeMs: composite.ElapsedTimeMs,
		Error:         composite.Error,
		Timestamp:     composite.Timestamp,
		HttpCode:      composite.HTTPCode,
		Text:          string(composite.TextContent),
		Title:         string(composite.Title),
	}

	if composite.Language != nil {
		page.Language = composite.Language.Name
	}

	return []*types.Indexable{
		{
			ID:         fmt.Sprintf("%x", md5.Sum([]byte(composite.URI))),
			Index:      s.Name(),
			DataType:   s.Name(),
			Streamable: s.Streamable(),
			Data:       page,
		},
	}, nil
}

func (s *pageTransformer) Input() types.MessageType {
	return types.CompositeAnalysisType
}

func (s *pageTransformer) Streamable() bool {
	return true
}

func (s *pageTransformer) Name() string {
	return types.PageIndexable
}
