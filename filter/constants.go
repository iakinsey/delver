package filter

const articleDefaultDaysLookback = 90
const pageDefaultDaysLookback = 1

var articleDefaultFields = []string{"title", "url", "url_md5", "found"}
var pageDefaultFields = []string{"url", "domain", "http_code", "timestamp", "elapsed_time", "title"}

const queryTemplate = `{
    "from": 0,
    "size": 10000,
    "sort": [
        {"found": {"order": "desc"}}
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