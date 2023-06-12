package filter

const compositeDefaultDaysLookback = 90

var compositeDefaultFields = []string{
	"uri",
	"host",
	"http_code",
	"timestamp",
	"elapsed_time_ms",
	"features.title",
}

const queryTemplate = `{
    "from": 0,
    "size": 10000,
    "sort": [
        {"timestamp": {"order": "asc"}}
    ],
	"_source": %s,
    "query": {
        "bool": {
            "must": %s
        }
	}
}`

const metricQueryTemplate = `{
    "size": 0,
    "query": {
        "bool": {
            "must": [
                {
                    "match": {
                        "key": "%s"
                    }
                },
                {
                    "range": {
                        "when": {
                            "gte": %d,
                            "lte": %d
                        }
                    }
                }
            ]
        }
    },
    "aggs": {
        "metric_timeseries": {
            "date_histogram": {
                "field": "when",
                "fixed_interval": "%s"
            },
            "aggs": {
                "metric_rollup": {
                    "%s": {
                        "field": "value"
                    }
                }
            }
        }
    }
   
}`
