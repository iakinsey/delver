package transformers

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/pkg/errors"
)

const ngramDelimeter = "0000"

type articleTransformer struct{}

func NewArticleTransformer() Transformer {
	return &articleTransformer{}
}

func (s *articleTransformer) Perform(msg json.RawMessage) ([]*types.Indexable, error) {
	composite := message.CompositeAnalysis{}
	ngramFeature := features.Ngrams{}
	sentiment := features.Sentiment{}

	if err := json.Unmarshal(msg, &composite); err != nil {
		return nil, errors.Wrap(err, "transformer failed to parse article")
	}

	article := types.Article{
		Summary:   "",
		Url:       composite.URI,
		UrlMd5:    fmt.Sprintf("%x", md5.Sum([]byte(composite.URI))),
		OriginUrl: composite.Origin,
		Type:      s.Name(),
		Found:     composite.Timestamp,
	}

	composite.LoadPermissive(features.TextField, &article.Content)
	composite.LoadPermissive(features.TitleField, &article.Title)
	composite.LoadPermissive(features.CountryField, &article.Countries)
	composite.LoadPermissive(features.CompanyNameField, &article.Corporate)

	if ok := composite.LoadPermissive(features.NgramField, &ngramFeature); ok {
		ngrams := make([]string, 0)

		if ngramsAsList, ok := ngramFeature[3]; ok {
			for _, tokens := range ngramsAsList {
				ngrams = append(ngrams, strings.Join(tokens, ngramDelimeter))
			}

			article.Ngrams = ngrams
		}
	}

	if ok := composite.LoadPermissive(features.SentimentField, &sentiment); ok {
		if sentiment.BinaryNaiveBayesAggregate != nil {
			article.BinarySentimentNaiveBayesAggregate = int(*sentiment.BinaryNaiveBayesAggregate)
		}
		if sentiment.BinaryNaiveBayesTitle != nil {
			article.BinarySentimentNaiveBayesTitle = int(*sentiment.BinaryNaiveBayesTitle)
		}
		if sentiment.BinaryNaiveBayesContent != nil {
			article.BinarySentimentNaiveBayesContent = int(*sentiment.BinaryNaiveBayesContent)
		}
		if sentiment.BinaryNaiveBayesSummary != nil {
			article.BinarySentimentNaiveBayesSummary = int(*sentiment.BinaryNaiveBayesSummary)
		}
	}

	return []*types.Indexable{
		{
			ID:         article.UrlMd5,
			Index:      s.Name(),
			DataType:   s.Name(),
			Streamable: s.Streamable(),
			Data:       article,
		},
	}, nil
}

func (s *articleTransformer) Input() types.MessageType {
	return types.CompositeAnalysisType
}

func (s *articleTransformer) Streamable() bool {
	return true
}

func (s *articleTransformer) Name() string {
	return types.ArticleIndexable
}
