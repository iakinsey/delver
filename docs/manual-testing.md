# Manual testing with Elasticsearch

These commands run from the repository root. Elasticsearch must already be listening on `http://localhost:9200`; the local fixture, API, subscription server, and development UI use ports 8088, 8080, 26548, and 5173. Stop previous Delver instances before starting these examples.

The live regression was verified against Elasticsearch `9.5.6-SNAPSHOT`. It checks real mappings, repeated index declaration, bulk and single-document indexing, stable IDs, mapping rejection, exact/literal filters, index isolation, second-based sum/average buckets, and historical-to-live WebSocket aggregation. Each run creates uniquely named `delver-live-*` indices and deletes only its own indices afterward.

## 1. Run the live regression

```sh
curl -fsS http://localhost:9200/ | python3 -m json.tool
DELVER_ES_URL=http://localhost:9200 go test -race -v -count=1 -timeout 90s ./gateway -run '^TestLiveElasticsearch$'
```

All four subtests should pass. The normal unit suite still runs without a cluster; this test skips unless `DELVER_ES_URL` is set. `make test-live-es` is a shorthand for the live regression.

## 2. Start the local website — terminal A

```sh
go run ./examples/site
```

Keep it running. `http://localhost:8088/feed.xml` contains one local article. Its title sentiment is deliberately allowed to be zero; zero is valid data, not a missing feature.

## 3. Start the crawler — terminal B

```sh
go build -o build/delver .
python3 scripts/manual-es-config.py
./build/delver run-data/es-manual/config.json
```

The generator creates a new crawl namespace, enables metrics and indexing, uses separate local storage, and republishes the feed every five seconds. It does not delete existing records. Actual refetches can occur every second publication because recrawling is measured from completion of the preceding fetch.

Expect `transformer indexed ... entities` messages. The crawler writes the article to `composite`, counters/samples to `metric`, and article envelopes to `run-data/es-manual/results.jsonl`. These application indices remain available for inspection.

## 4. Inspect Elasticsearch — terminal C

```sh
curl -fsS 'http://localhost:9200/_cat/indices/composite,metric?v'

curl -fsS http://localhost:9200/composite/_search \
  -H 'Content-Type: application/json' \
  -d '{"size":3,"sort":[{"timestamp":"desc"}],"query":{"term":{"host":"localhost:8088"}}}' \
  | python3 -m json.tool

curl -fsS http://localhost:9200/metric/_search \
  -H 'Content-Type: application/json' \
  -d '{"size":5,"sort":[{"when":"desc"}],"query":{"term":{"key":"httpFetcher.message.in"}}}' \
  | python3 -m json.tool
```

The article should have a title, `features.country: ["USA"]`, English language metadata, and `features.sentiment.binary_naive_bayes_title`. Metrics should contain numeric values with Unix-second timestamps. Elasticsearch search visibility follows its refresh interval, so a newly indexed record can take a moment to appear. A single-node cluster can report yellow for the application indices because their default replica cannot be assigned; primary indexing and searches still work.

## 5. Test preload and live subscriptions

The included Go subscriber connects directly. Configure subscriber origins in `streamer.allowed_origins`.

```sh
go run ./examples/subscribe \
  -filter '[{"data_type":"composite","query":{"domain":["localhost:8088"]},"callback":"feed","options":{"preload":true}}]'

go run ./examples/subscribe -count 3 -timeout 30s \
  -filter '[{"data_type":"metric","query":{"key":"httpFetcher.message.in"},"callback":"fetch-count","options":{"preload":true}}]'

go run ./examples/subscribe -count 3 -timeout 30s \
  -filter '[{"data_type":"composite","query":{"domain":["localhost:8088"]},"fields":["timestamp","features.sentiment.binary_naive_bayes_title"],"callback":"sentiment","agg":{"agg_name":"avg","time_field":"timestamp","agg_field":"features.sentiment.binary_naive_bayes_title","time_window_seconds":5},"options":{"preload":true}}]'
```

Each command prints JSON envelopes. Subscription acknowledgment and preload-complete control messages accompany historical and live data. The sentiment preload includes the newest, still-open window. The metric key is exact, not a wildcard. Let the crawler fetch at least once before running these probes.

## 6. Inspect crawls in the UI

Open `http://localhost:8080` to use the embedded crawl workspace. Open the imported crawl to inspect its run status, results, operations, and errors. The dashboard builder and widget canvas have been removed; indexed Elasticsearch data remains inspectable through the queries and subscriptions above.

For frontend development, start a separate UI process:

```sh
cd web
npm ci --legacy-peer-deps
npm start
```

Open `http://localhost:5173`. Its default API target is port 8080. Use `VITE_API_SERVER` to select a different API.

## 7. Stop and rerun

Press Ctrl-C in each server terminal. Restart the crawler with the same configuration to resume. Run the configuration generator again while the crawler is stopped to start a new crawl namespace without deleting prior data. The test indices and application data are distinct: the regression cleans up its unique indices, while the application retains `composite`, `metric`, dashboard storage, and local output for inspection.
