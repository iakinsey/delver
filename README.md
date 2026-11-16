# Delver

Delver is a configurable Go web crawler. JSON configuration connects seed publishers, HTTP fetchers, dependent feature extractors, discovery workers, and output sinks through queues. Use the same components from Go through the `application` package, or register your own workers, resources, and extractors.

Persistent crawl queues deduplicate normalized URLs exactly and remember completed work across restarts. File queues preserve pipeline messages and staged output batches. Channel queues are available for transient, in-process pipelines. Delivery is at least once: a failure can replay a result, so external sinks should use stable message or request IDs.

## Start the application

Run `go run .` and open http://127.0.0.1:8080. Create and configure independent crawls in the bundled UI; no scenario JSON is required. Per-crawl controls include traversal, scope, completion, retention, and authenticated HTTP/HTTPS or SOCKS5 proxies. See the [workspace guide](docs/workspace.md).

## Run a local crawl

Install Go 1.23 or newer and a C compiler; SQLite support uses CGO. From the repository root:

```sh
go build -o build/delver .
go run ./examples/site
```

Leave the example site running. In another terminal:

```sh
./build/delver examples/minimal.json
```

The example fetches `http://localhost:8088/`, follows its child link, deduplicates the repeated link and cycle, and records the robots-denied request without fetching that page. Analyses appear as JSON message envelopes in `run-data/minimal/results.jsonl`. Each line's `message` contains request/response metadata and `features` such as `title`, `text`, and `url`.

Press Ctrl-C to stop. Run the same command again to resume the same crawl; completed URLs are retained. Use a new crawl ID to revisit the site, or configure `recrawl_after` for periodic revisits. The process stays running for scheduled publishers until stopped.

## Examples

| Configuration | Behavior | Additional services |
| --- | --- | --- |
| [minimal](examples/minimal.json) | Local site, depth limit, exact deduplication, JSONL results | None |
| [persistent DFS](examples/dfs.json) | Rotates through stored domains and resumes persistent work | None |
| [RSS/news](examples/news.json) | Local RSS feed, language/company/country/title sentiment extraction, article output, indexing, dashboard APIs | Elasticsearch (live-tested on 9.5.6-SNAPSHOT) |

Run examples from the repository root. Each uses a separate directory beneath `run-data`. The news example explicitly configures company/country data paths; include those files or configure their paths when deploying a binary elsewhere.

## Features

- Fixed and RSS/Atom seeds, periodic jobs, depth-limited domain discovery, and configurable news collection.
- HTTP/HTTPS fetching with proxies, retries, robots rules, scope restrictions, request spacing, concurrency limits, redirect checks, and response-size limits.
- HTML links, text, titles, language, country/company mentions, ngrams, title sentiment, and adversarial-link indicators.
- Persistent exact crawl state, recoverable file queues with retries and dead letters, transient channel queues, Badger maps, and optional approximate Bloom filters.
- JSONL output, Elasticsearch indexing, dashboard APIs, and filtered WebSocket subscriptions.

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
- [API and subscriptions](docs/api.md)

## Development

```sh
go test -timeout 90s ./...
go test -race -timeout 120s ./...
go vet ./...
```

Tests use temporary storage and local HTTP servers. The application integration test checks discovery, robots denial, redirects, exact deduplication, output, and shutdown without Elasticsearch. Search tests simulate Elasticsearch responses; the opt-in live regression also checks a real cluster. See [web/README.md](web/README.md) for crawl UI commands.

Stored Bloom formats and configuration fields have changed during this repair. Start with the supplied examples and fresh storage; no migration of earlier experimental state is provided.
