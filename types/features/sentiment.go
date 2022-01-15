package features

type Sentiment struct {
	BinaryNaiveBayesSummary *uint8   `json:"binary_naive_bayes_summary"`
	BinaryNaiveBayesContent *uint8   `json:"binary_naive_bayes_content"`
	BinaryNaiveBayesTitle   *uint8   `json:"binary_naive_bayes_title"`
	NaiveBayesAggregate     *float32 `json:"naive_bayes_aggregate"`
}
