# Crawl workspace

Run `go run .` from the repository root and open http://127.0.0.1:8080. No scenario file, Node process, or Elasticsearch service is required. The Go executable embeds the built UI. `make` rebuilds both UI and executable; use Node 22.22 or newer when changing the UI.

The UI opens directly into the crawl workspace. The old dashboard builder has been removed; old dashboard links redirect here.

The library starts empty. Create a crawl, give it a name, and enter one starting URL or RSS/Atom feed per line. Choose the boundary (host, domain, or open web), traversal strategy, and completion behavior. The rule preview shows how example URLs match your boundary and patterns. Advanced settings expose depth, domain hops, pacing, parallel requests, and retention. The summary explains the resulting behavior before starting.

Each crawl has independent configuration, drafts, queues, frontier, results, runtime metrics, and controls. Multiple crawls can run together. Pausing one does not pause another. A shared per-origin gate limits aggregate traffic to two concurrent requests and 100ms spacing; a crawl can request stricter pacing. Active duration budgets exclude paused time and application downtime.

## Completion and recovery

The default monitors its sources and revisits pages every 24 hours. Choose finish when drained for a finite crawl, or set request and active-duration limits. Reaching a limit stops new dispatch, drains work already sent to workers, and preserves remaining discoveries. The completed run shows its queue snapshot. Continue pending work resumes the latest run's storage with limits lifted, source polling disabled, and finite drain behavior. A new run instead uses separate storage and the current draft.

Draft edits do not change a running crawl. Apply the draft to create a new versioned run. Pause, resume, and stop operate on the selected crawl. Operations are asynchronous and their status is visible in the UI. Interrupted operations are reported after restart. Archived definitions and historical run previews remain available.

Storage defaults to `run-data/workspace`. `crawls.json` stores definitions, versions, and desired lifecycle state; each crawl/run has its own data directory. A workspace lock prevents two processes opening the same library. Back up the entire directory with the process stopped. The default retention expires completed frontier details after 7 days, failed frontier details after 30 days, and deduplication history after 90 days. Choose never for permanent retention. Pending and reserved work is protected; expiration does not use an offset cursor that can skip surviving records. Retention is not a hard disk quota: queued work, results, and retained history still require capacity. See [frontier storage](frontier.md).

## Deleting crawls

Open a crawl and choose **Delete crawl**, then confirm **Delete permanently**. Deletion stops the runtime and removes that crawl's local configuration, queues, frontier, bodies, results, operations, and all run history. Archived crawls can also be deleted. Other crawls are preserved.

**Preserve crawl logs** is checked by default and keeps Elasticsearch records. Uncheck it to delete this crawl's Elasticsearch records from the configured indexing clusters. Shared indices are never deleted. New runs attach crawl/run identities to indexed records. Older indexing runs without attribution cannot be safely purged: keep the checkbox checked to delete their local data. Custom external sinks require their own cleanup.

Deletion runs in the background. Errors remain visible in run activity and can be retried with **Delete crawl**. Interrupted deletion does not resume crawling after restart. Imported legacy queues, object storage, result files, and operations state are also removed when exclusively owned; ambiguous/shared storage paths are rejected. Original import configuration files are input files, and remain on disk; explicitly importing them again can create a new crawl.

## Proxies

Connection settings support direct access, HTTP/HTTPS proxies, and SOCKS5 proxies, including credentials in the proxy URL. These settings apply to page requests, redirects, robots files, and feeds for that crawl. The editor and configuration API show the complete proxy address, including credentials. Editing the address changes it. Diagnostic error text still removes credentials. Workspace files contain the operational credentials and are created with private permissions.

### Private namespaces and slower networks

The crawler uses the existing SOCKS5 connection; it does not launch or manage Tor. For an externally managed Tor SOCKS listener, choose **SOCKS5** and enter, for example, `socks5h://127.0.0.1:9050` (use your listener's actual address). Both `socks5://` and `socks5h://` send destination hostnames to the proxy without local destination DNS resolution. All built-in page, feed, robots, and redirect requests use the crawl's connection. A configured proxy failure never falls back to a direct connection.

The guided editor exposes independent controls:

- **Respect robots.txt** defaults to enabled. Turn it off to skip robots requests and rules for pages and feeds. Existing definitions without this setting continue respecting robots. Save a replacement run to apply a draft change; continuing an existing run retains its saved configuration.

- **Require a proxy connection** rejects a configuration without an explicit proxy. Keep this enabled when direct access is unwanted.
- Under **Preview boundaries and exclude URLs**, enable **Restrict destination hostnames** and enter `onion` for an onion-only crawl. Suffixes match entire DNS labels: `onion` permits `example.onion`, not `example.onion.example.com`. Rules also reject incompatible starting URLs/feeds, filter discovered URLs/feed articles, and block redirect destinations and robots requests before connecting. Boundary and pattern rules still apply separately.
- Under **Request timeouts and retries**, a starting point for slow destinations is a `120s` timeout, `2` retries, `1s` initial delay, and `30s` maximum delay. Defaults remain `10s`, `1`, `100ms`, and `5s`. Timeouts start after pacing; `Retry-After` takes precedence over the backoff cap. Existing request pacing can reduce concurrency to 1 and space starts by `1s` or more. Tune these to your sources.
- **User-Agent** sets the header on built-in requests; changing it does not provide browser anonymity.
- For a SOCKS proxy that supports authentication-based session isolation, **Separate proxy sessions for each run** supplies generated username/password labels. Pages, feeds, and robots share the run's labels. Stop/resume and reopening the same run storage retain them; a new run gets different labels. Leave proxy URL credentials empty with this option, since it cannot replace real login credentials. The proxy must accept the labels and implement isolation; the crawler does not promise a new circuit or anonymity.

These controls are per crawl and preserve old saved definitions' defaults. They operate through the worker runtime's HTTP configuration, so custom networking code must use that configuration and the shared HTTP client to observe them. They are not a process-wide firewall. No live Tor connection is required for the local regression tests: the SOCKS fixture checks hostname forwarding, labels, and destination enforcement over the actual SOCKS wire protocol.

## Custom pipelines and migration

The expert editor preserves the existing worker/resource configuration model and unknown component parameters. Contextual registration APIs pass runtime configuration and metric sinks to custom components. Legacy registration remains supported. Expert runs can use custom workers; finite termination controls require verifiable source completion, and request budgets require a single built-in frontier. Validation explains unsupported combinations before activation.

An optional argument, `go run . examples/minimal.json`, imports a legacy pipeline into the library while retaining its existing storage paths and saved state. Repeating the same import does not create another copy. Overlapping storage ownership is rejected. Start with a copy of valuable legacy data when evaluating migration. The legacy application APIs remain available; singleton crawler HTTP aliases require exactly one crawl.

## API and results

The workspace API uses POST requests under `/crawls`: `defaults`, `preview`, `list`, `create`, and crawl-scoped actions including `detail`, `draft`, `pipeline`, `activate`, `start_run`, `continue_run`, `command`, `status`, `operations`, `results`, `duplicate`, `archive`, and `delete`. Mutations use optimistic revisions; asynchronous operations accept idempotency IDs. The UI supplies these automatically.

Each run writes `results.jsonl`. The UI shows bounded recent previews, including historical runs, rather than loading the full output into memory. External sinks should still handle at-least-once delivery. No workspace endpoint silently searches other crawl storage.

## Fetch capacity and monitoring

Use **Fetch throughput** to set overall capacity independently of host pacing, choose fixed/adaptive control, and configure shared limits, pressure, phase timeouts, retry recovery, and bounded metrics. New crawls default to CPU count × 4. Older saved crawls retain their worker count until explicitly configured. The expert editor exposes the same controls with an explicit apply action. See [fetch throughput](fetch-throughput.md) for defaults, interactions, persistence, and verification.
