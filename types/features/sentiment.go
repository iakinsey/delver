package features

type Sentiment struct {
	BinaryNaiveBayesSummary   *int32 `json:"binary_naive_bayes_summary"`
	BinaryNaiveBayesContent   *int32 `json:"binary_naive_bayes_content"`
	BinaryNaiveBayesTitle     *int32 `json:"binary_naive_bayes_title"`
	BinaryNaiveBayesAggregate *int32 `json:"binary_naive_bayes_aggregate"`
}
