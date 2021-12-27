package features

type Sentiment struct {
	BinarySentimentNaiveBayesSummary   *float32
	BinarySentimentNaiveBayesContent   *float32
	BinarySentimentNaiveBayesTitle     *float32
	BinarySentimentNaiveBayesAggregate *float32
}
