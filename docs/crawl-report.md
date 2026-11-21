# Crawl report

Open **Report** from a crawl or its run history. Reports belong to runs; changing the selected run changes its counters and graph.

- **Requests/s** counts completed attempts, including failures and retries. A running run uses the existing rolling 60-second rate. A finished run uses attempts divided by running time, excluding paused time.
- **URLs visited** counts distinct canonical URLs that returned an HTTP response, including redirects, HTTP errors, and responses whose bodies failed. Retries do not increase this count.
- **URLs not visited** is discovered URLs minus visited URLs. It includes pending URLs and terminal connection failures. Frontier expiration does not erase report counts.

## Host explorer

Focus **Search hosts**, enter a hostname or URL, and select a result. Search uses an indexed hostname prefix and orders matches by distinct connected hosts, descending, counting a reciprocal link once. Ties sort by hostname. **More** continues that order. **Depth** counts links in either direction; arrows retain their original direction. Scheme and port share a hostname node, while subdomains remain separate. Hostnames are lowercased and normalized with IDNA. Same-host links do not create graph edges.

Node size represents global PageRank authority, independent of the displayed neighborhood. Amber rings mark connections to other communities. The inspector shows authority percentile, incoming/outgoing degree, other connected communities, and URL counts. Bridge importance describes this cross-community role, not shortest-path betweenness. Communities use deterministic, degree-normalized label propagation with at most twelve passes. These are structural estimates, not content quality scores.

**Hosts** is the default. When the neighborhood exceeds display limits, the explorer chooses actual hosts across depth and community strata, favoring authority and connectors and retaining connecting paths. The full neighborhood is counted; the display states when it is a structural sample. Searching any retained host recenters on it even if it was absent from the sample.

**Grouped** is optional. Oversized neighborhoods retain important host anchors and combine remaining hosts into expandable groups. Group membership and directed link totals include all hosts and links in the neighborhood, including internal group links. Expansion redistributes the display budget; an expansion that cannot fit reports an error. Group size does not represent PageRank.

**Display settings** defaults to 500 nodes and 2,000 links; supported ranges are 10–2,000 nodes and 32–10,000 links. WebGL renders only the bounded projection. Layout runs in a worker for a bounded interval. Pan, zoom, Fit, Fullscreen, keyboard controls, and a selectable host/group list are available. Server analysis is bounded at 100,000 hosts and 1,000,000 distinct directed links per run; larger runs receive an explicit analysis-limit error. Counter and host-search endpoints remain available.

## Storage and integration

Each run stores a SQLite WAL ledger beside its state file as `state.json.report.sqlite`. It stores SHA-256 canonical URL identities and response bits, hostname counters, and distinct directed host links. Discovery records links before frontier deduplication; receipt recording occurs when response headers arrive. Robots metadata requests do not count. Batches are bounded, writes are transactional, and replay is idempotent. Independent handles within the process share a per-ledger writer lock, so live discovery and response receipts can run between historical import batches without competing for SQLite writer admission. Report storage is independent of frontier retention and is deleted with run/crawl storage.

`RuntimeDependencies.Reporter` provides `report.Writer` to custom resources and workers. Workers may implement `SetReporter(report.Writer)` or use that dependency directly. Custom fetchers must report responses and accepted discoveries through this interface. No additional worker queue is required.

Older runs import retained frontier/staged records, JSONL output, and attributed Elasticsearch composite history in bounded background pages with durable checkpoints. The report labels historical coverage **Partial** because expired records and missing historical links cannot be reconstructed reliably. Indexed history must carry the crawl and run identity; unscoped documents are never imported. Elasticsearch errors are reported and do not prevent local-history import.

## API and validation

The existing workspace RPC envelope exposes `/crawls/report`, `/crawls/hosts`, and `/crawls/host_graph`. Supply `crawl_id` and optional `run_id` (latest by default). Host search accepts `query`, `after` (the last result’s `cursor`; older hostname cursors are also accepted), and `limit`. Graph accepts `host`, `depth` (default 2), `mode` (`hosts` or `grouped`), `node_limit`, `edge_limit`, and optional `expanded` group IDs. Responses contain compact report/graph data rather than result payloads. Graph analysis has a 30-second deadline and a two-entry topology cache; live URL counters are read independently.

Normal unit and browser suites include report coverage. Scale workloads start from empty temporary storage and can also be selected directly with Go:

```sh
make test-report-million
make test-report-million-disk
make test-report-graph
make test-elasticsearch ES_URL=http://127.0.0.1:19200
```

Set `VALIDATION_ROOT` for generated JSON evidence and `DISK_ROOT` for persistent test storage. Evidence stays outside Git. The graph scale case checks a 100,000-host, 1,000,000-link topology, bounded rendering, grouped conservation, memory, and the API deadline. Capacity benchmarks `BenchmarkWorkspaceCapacity` and `BenchmarkWorkspaceCapacityWithReport` measure fixed-mode execution with and without concurrent report reads.

The Elasticsearch gate includes a live report-history scroll test spanning multiple pages, crawl/run isolation, failed and HTTP-error receipts, replay, and cleanup of its own documents. Supply an isolated instance.
