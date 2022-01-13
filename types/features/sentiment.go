package features

type Sentiment struct {
	BinaryNaiveBayesSummary *uint8
	BinaryNaiveBayesContent *uint8
	BinaryNaiveBayesTitle   *uint8
	NaiveBayesAggregate     *float32
}
