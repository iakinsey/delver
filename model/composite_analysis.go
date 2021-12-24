package model

import (
	"github.com/iakinsey/delver/model/features"
	"github.com/iakinsey/delver/types"
)

type CompositeAnalysis struct {
	Adversarial   *features.Adversarial
	Blacklist     *features.Blacklist
	Corporations  *[]string
	Countries     *[]string
	Language      *features.Language
	Ngrams        *map[int32][]string
	TermFrequency *map[string]string
	TextContent   *string
	Sentiment     *features.Sentiment
	URIs          *[]types.URI
}
