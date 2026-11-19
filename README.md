# Delver

Delver is a configurable Go web crawler. JSON configuration connects seed publishers, HTTP fetchers, dependent feature extractors, discovery workers, and output sinks through queues. Use the same components from Go through the `application` package, or register custom workers, resources, and extractors.

Persistent crawl queues deduplicate normalized URLs exactly and remember completed work across restarts. File queues preserve pipeline messages and staged output batches. Channel queues are available for transient, in-process pipelines. Delivery is at least once: a failure can replay a result, so external sinks should use stable message or request IDs.

## Start the application

Run `go run .` and open http://127.0.0.1:8080. Create and configure independent crawls in the bundled UI; no scenario JSON is required. Per-crawl controls include traversal, scope, completion, retention, and authenticated HTTP/HTTPS or SOCKS5 proxies. See the [workspace guide](docs/workspace.md).

To listen on network interfaces, run `./build/delver -listen 0.0.0.0:8080`.

## Run a local crawl

Install Go 1.23 or newer and a C compiler; SQLite support uses CGO. From the repository root:

```sh
go build -o build/delver .
go run ./examples/site
```

Leave the site running. Start `./build/delver`, create a crawl in the UI, and use `http://localhost:8088/` as a seed. Select **Drain** for finite execution. Results appear in the selected run's **Results** view and its `results.jsonl` file under `run-data/workspace`.

Custom Go components are demonstrated in [examples/custom](examples/custom/main.go). Runnable synthetic pipeline configurations live under `application/testdata/examples`; they are regression fixtures, not deployment settings. The one-time importer accepts a private pipeline file as a CLI argument.

## Features

- Fixed and RSS/Atom seeds, periodic jobs, depth-limited domain discovery, and configurable news collection.
- HTTP/HTTPS fetching with proxies, retries, robots rules, scope restrictions, request spacing, concurrency limits, redirect checks, and response-size limits.
- HTML links, text, titles, language, country/company mentions, ngrams, title sentiment, and adversarial-link indicators.
- Persistent exact crawl state, recoverable file queues with retries and dead letters, transient channel queues, Badger maps, and optional approximate Bloom filters.
- JSONL output, Elasticsearch indexing, scoped log deletion, and bounded historical result previews.

## Documentation

- [Crawl workspace](docs/workspace.md)
- [Frontier strategies and storage](docs/frontier.md)
- [Fetch throughput and validation](docs/fetch-throughput.md)
- [Configuration reference](docs/configuration.md)
- [Architecture and delivery semantics](docs/architecture.md)
- [Custom components](docs/extending.md)
- [Operations and recovery](docs/operations.md)
- [Crawler control UI and API](docs/crawler-operations.md)
- [Capability audit](docs/capability-audit.md)
- [Live Elasticsearch and manual testing](docs/manual-testing.md)
- [Workspace API](docs/api.md)

## Development

```sh
make test-setup
make build
make test
make test-race
make test-static
make test-ui
```

Tests use temporary storage and controlled network fixtures. See [browser validation](web/README.md) for targeted UI coverage and [validation commands](docs/manual-testing.md) for benchmarks, endurance, scale, Elasticsearch, and Tor. Tests and synthetic fixtures are tracked; generated evidence and private configuration stay outside Git.
