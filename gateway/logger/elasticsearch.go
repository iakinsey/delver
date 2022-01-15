package logger

type ElasticsearchLogger interface {
}

type elasticsearchLogger struct {
	uri string
}

func NewElasticsearchLogger(addresses []string) Logger {
	return nil
	/*
		client, err := elasticsearch.NewClient(elasticsearch.Config{
			Addresses: addresses,
		})

		if err != nil {

		}
	*/
}
