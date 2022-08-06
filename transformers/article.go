package transformers

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iakinsey/delver/types"
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

	if err := json.Unmarshal(msg, &composite); err != nil {
		return nil, errors.Wrap(err, "transformer failed to parse article")
	}

	article := types.Article{
		Summary:   "",
		Content:   string(composite.TextContent),
		Title:     string(composite.Title),
		Url:       composite.URI,
		UrlMd5:    fmt.Sprintf("%x", md5.Sum([]byte(composite.URI))),
		OriginUrl: composite.Origin,
		Type:      types.ArticleIndexable,
		Found:     composite.Timestamp,
		Countries: composite.Countries,
		Corporate: composite.Corporations,
	}

	if composite.Ngrams != nil {
		ngramMap := *composite.Ngrams
		ngrams := make([]string, 0)

		if ngramsAsList, ok := ngramMap[3]; ok {
			for _, tokens := range ngramsAsList {
				ngrams = append(ngrams, strings.Join(tokens, ngramDelimeter))
			}
		}

		article.Ngrams = ngrams
	}

	if composite.Sentiment != nil {
		sentiment := composite.Sentiment

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
			Index:      types.ArticleIndexable,
			DataType:   types.ArticleIndexable,
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
