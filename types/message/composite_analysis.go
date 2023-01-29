package message

const (
	AdversarialExtractor string = "adversarial"
	CompanyNameExtractor string = "company_name"
	CountryExtractor     string = "country"
	LanguageExtractor    string = "language"
	SentimentExtractor   string = "sentiment"
	TextExtractor        string = "text"
	NgramExtractor       string = "ngram"
	UrlExtractor         string = "url"
	TitleExtractor       string = "title"
)

type CompositeAnalysis struct {
	FetcherResponse

	Features map[string]interface{} `json:"features"`

	/*
		Adversarial  *features.Adversarial `json:"adversarial,omitempty"`
		Corporations features.Corporations `json:"corporations,omitempty"`
		Countries    features.Countries    `json:"countries,omitempty"`
		Language     *features.Language    `json:"language,omitempty"`
		TextContent  features.TextContent  `json:"text_content,omitempty"`
		Title        features.Title        `json:"title,omitempty"`
		Sentiment    *features.Sentiment   `json:"sentiment,omitempty"`
		Ngrams       *features.Ngrams      `json:"ngrams,omitempty"`
		URIs         features.URIs         `json:"uris,omitempty"`
	*/
}

func (s *CompositeAnalysis) Has(key string) bool {
	_, ok := s.Features[key]

	return ok
}

func (s *CompositeAnalysis) Get(key string) interface{} {
	return s.Features[key]
}

func (s *CompositeAnalysis) GetList(key string) []interface{} {
	return s.Get(key).([]interface{})
}

var ParquetSchema = `{
	"Tag": "name=resource, repetitiontype=REQUIRED",
	"Fields": [
		{"Tag": "name=request_id, inname=RequestID, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=uri, inname=URI, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=host, inname=Host, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=title, inname=Title, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=origin, inname=Origin, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=protocol, inname=Protocol, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=store_key, inname=StoreKey, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=content_md5, inname=ContentMD5, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=elapsed_time_ms, inname=ElapsedTimeMs, type=INT64, convertedtype=INT_64, repetitiontype=REQUIRED"},
		{"Tag": "name=error, inname=Error, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=http_code, inname=HTTPCode, type=INT32, convertedtype=UINT_16, repetitiontype=REQUIRED"},
		{"Tag": "name=success, inname=Success, type=BOOLEAN, repetitiontype=REQUIRED"},
		{"Tag": "name=timestamp, inname=Timestamp, type=INT64, convertedtype=INT_64, repetitiontype=REQUIRED"},
		{
			"Tag": "name=header, inname=Header, type=MAP, repetitiontype=REQUIRED",
			"Fields": [
				{"Tag": "name=key, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
				{
					"Tag": "name=value, type=LIST, repetitiontype=REQUIRED",
					"Fields": [
						{"Tag": "name=element, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"}
					]
				}
			]
		},
		{
			"Tag": "name=adversarial, inname=Adversarial, repetitiontype=OPTIONAL",
			"Fields": [
				{"Tag": "name=enumeration, inname=Enumeration, type=BOOLEAN, repetitiontype=OPTIONAL"},
				{"Tag": "name=enumeration, inname=SubdomainExplosion, type=BOOLEAN, repetitiontype=OPTIONAL"}
			]
		},
    	{
			"Tag": "name=corporations, inname=Corporations, type=LIST, repetitiontype=REQUIRED",
			"Fields": [{"Tag": "name=element, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"}]
		},
    	{
			"Tag": "name=countries, inname=Countries, type=LIST, repetitiontype=REQUIRED",
			"Fields": [{"Tag": "name=element, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"}]
		},
		{
			"Tag": "name=language, inname=Language, repetitiontype=OPTIONAL",
			"Fields": [
				{"Tag": "name=name, inname=Name, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
				{"Tag": "name=confidence, inname=Confidence, type=DOUBLE, repetitiontype=REQUIRED"}
			]
		},
		{"Tag": "name=text_content, inname=TextContent, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{
			"Tag": "name=sentiment, inname=Sentiment, repetitiontype=OPTIONAL",
			"Fields": [
				{"Tag": "name=binary_naive_bayes_content, inname=BinaryNaiveBayesContent, type=INT32, repetitiontype=OPTIONAL"}
			]
		},
		{
			"Tag": "name=uris, inname=URIs, type=LIST, repetitiontype=REQUIRED",
			"Fields": [{"Tag": "name=element, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"}]
		}
	]
}`
