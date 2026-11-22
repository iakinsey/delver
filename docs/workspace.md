# Crawl workspace

Run `go run .` from the repository root and open http://127.0.0.1:8080. No scenario file, Node process, or Elasticsearch service is required. The Go executable embeds the built UI. `make` rebuilds both UI and executable; use Node 22.22 or newer when changing the UI.

The UI opens directly into the crawl workspace. Legacy dashboard and singleton crawler APIs are removed.

The library starts empty. Create a crawl, give it a name, and enter one starting URL or RSS/Atom feed per line. Choose the boundary (host, domain, or open web), traversal strategy, and completion behavior. The rule preview shows how example URLs match your boundary and patterns. Advanced settings expose depth, domain hops, pacing, parallel requests, and retention. The summary explains the resulting behavior before starting.

Each crawl has independent configuration, drafts, queues, frontier, results, runtime metrics, and controls. Multiple crawls can run together. Pausing one does not pause another. The shared per-origin gate defaults to two concurrent requests and 100ms spacing. Configure shared ceilings in Execution limits; a crawl can request stricter pacing. Active duration budgets exclude paused time and application downtime.

## Completion and recovery

The default monitors its sources and revisits pages every 24 hours. Choose finish when drained for a finite crawl, or set request and active-duration limits. Reaching a limit stops new dispatch, drains work already sent to workers, and preserves remaining discoveries. The completed run shows its queue snapshot. Continue pending work resumes the latest run's storage with limits lifted, source polling disabled, and finite drain behavior. Start, Resume, Restart, and applying a saved configuration reuse the latest run and its storage. Saved configuration changes apply on Start or Resume. Request and duration limits remain cumulative; raise or remove an exhausted limit to continue under the updated configuration. Duplicate creates a separate crawl.

Draft edits do not change a running crawl. Apply the draft, or Start/Resume, to load the configuration in the same run. Pause, resume, and stop operate on the selected crawl. Operations are asynchronous and their status is visible in the UI. Interrupted operations are reported after restart. Archived definitions and historical run previews remain available.

Storage defaults to `run-data/workspace`. `crawls.json` stores definitions, versions, and desired lifecycle state; each crawl/run has its own data directory. A workspace lock prevents two processes opening the same library. Back up the entire directory with the process stopped. The default retention expires completed frontier details after 7 days, failed frontier details after 30 days, and deduplication history after 90 days. Choose never for permanent retention. Pending and reserved work is protected; expiration does not use an offset cursor that can skip surviving records. Retention is not a hard disk quota: queued work, results, and retained history still require capacity. See [frontier storage](frontier.md).

## Deleting crawls

Open a crawl and choose **Delete**, then confirm **Delete**. Deletion stops the runtime and removes that crawl's local configuration, queues, frontier, bodies, results, operations, and all run history. Archived crawls can also be deleted. Other crawls are preserved.

**Preserve crawl logs** is checked by default and keeps Elasticsearch records. Uncheck it to delete this crawl's Elasticsearch records from the configured indexing clusters. Shared indices are never deleted. New runs attach crawl/run identities to indexed records. Older indexing runs without attribution cannot be safely purged: keep the checkbox checked to delete their local data. Custom external sinks require their own cleanup.

Deletion runs in the background. Errors remain visible in run activity and can be retried with **Delete**. Interrupted deletion does not resume crawling after restart. Imported legacy queues, object storage, result files, and operations state are also removed when exclusively owned; ambiguous/shared storage paths are rejected. Original import configuration files are input files, and remain on disk; explicitly importing them again can create a new crawl.

## Proxies

Connection settings support direct access, HTTP/HTTPS proxies, and SOCKS5 proxies, including credentials in the proxy URL. These settings apply to page requests, redirects, robots files, and feeds for that crawl. The editor and configuration API show the complete proxy address, including credentials. Editing the address changes it. Diagnostic error text still removes credentials. Workspace files contain the operational credentials and are created with private permissions.

### Private namespaces and slower networks

The crawler uses the existing SOCKS5 connection; it does not launch or manage Tor. For an externally managed Tor SOCKS listener, choose **SOCKS5** and enter, for example, `socks5h://127.0.0.1:9050` (use your listener's actual address). Both `socks5://` and `socks5h://` send destination hostnames to the proxy without local destination DNS resolution. All built-in page, feed, robots, and redirect requests use the crawl's connection. A configured proxy failure never falls back to a direct connection.

The guided editor exposes independent controls:

- **Respect robots.txt** defaults to enabled. Turn it off to skip robots requests and rules for pages and feeds. Existing definitions without this setting continue respecting robots. Save a replacement run to apply a draft change; continuing an existing run retains its saved configuration.

- **Require proxy** rejects a configuration without an explicit proxy. Keep this enabled when direct access is unwanted.
- Under **URL filters**, enable **Restrict hostnames** and enter `onion` for an onion-only crawl. Suffixes match entire DNS labels: `onion` permits `example.onion`, not `example.onion.example.com`. Rules also reject incompatible starting URLs/feeds, filter discovered URLs/feed articles, and block redirect destinations and robots requests before connecting. Boundary and pattern rules still apply separately.
- Under **Requests**, a starting point for slow destinations is a `120s` timeout, `2` retries, `1s` initial delay, and `30s` maximum delay. Defaults remain `10s`, `1`, `100ms`, and `5s`. Timeouts start after pacing; `Retry-After` takes precedence over the backoff cap. Existing request pacing can reduce concurrency to 1 and space starts by `1s` or more. Tune these to your sources.
- **User-Agent** sets the header on built-in requests; changing it does not provide browser anonymity.
- For a SOCKS proxy that supports authentication-based session isolation, **Isolate proxy sessions** supplies generated username/password labels. Pages, feeds, and robots share the run's labels. Stop/resume and reopening the same run storage retain them; a new run gets different labels. Leave proxy URL credentials empty with this option, since it cannot replace real login credentials. The proxy must accept the labels and implement isolation; the crawler does not promise a new circuit or anonymity.

These controls are per crawl and preserve old saved definitions' defaults. They operate through the worker runtime's HTTP configuration, so custom networking code must use that configuration and the shared HTTP client to observe them. They are not a process-wide firewall. No live Tor connection is required for the local regression tests: the SOCKS fixture checks hostname forwarding, labels, and destination enforcement over the actual SOCKS wire protocol.

## Custom pipelines and migration

The expert editor preserves the existing worker/resource configuration model and unknown component parameters. Contextual registration APIs pass runtime configuration and metric sinks to custom components. Legacy registration remains supported. Expert runs can use custom workers; finite termination controls require verifiable source completion, and request budgets require a single built-in frontier. Validation explains unsupported combinations before activation.

An optional argument, `go run . /private/path/pipeline.json`, imports a legacy pipeline into the library while retaining its existing storage paths and saved state. Repeating the same import does not create another copy. Overlapping storage ownership is rejected. Start with a copy of valuable legacy data when evaluating migration. The Go registration and execution APIs remain available; `/crawler/*`, dashboard APIs, and streaming subscriptions are removed. Explicit legacy streaming output configurations return migration errors.

## API and results

The workspace API uses POST requests under `/crawls`: `defaults`, `preview`, `list`, `create`, and crawl-scoped actions including `detail`, `draft`, `pipeline`, `activate`, `start_run`, `continue_run`, `command`, `status`, `operations`, `results`, `duplicate`, `archive`, and `delete`. Mutations use optimistic revisions; asynchronous operations accept idempotency IDs. The UI supplies these automatically.

Each run writes `results.jsonl`. The UI shows bounded recent previews, including historical runs, rather than loading the full output into memory. External sinks should still handle at-least-once delivery. No workspace endpoint silently searches other crawl storage.

## Fetch capacity and monitoring

Use **Fetch throughput** to set overall capacity independently of host pacing, choose fixed/adaptive control, and configure pressure, phase timeouts, retry recovery, and bounded metrics. New crawls default to CPU count × 4. Older saved crawls retain their worker count until explicitly configured. The expert editor exposes the same controls with an explicit apply action. See [fetch throughput](fetch-throughput.md) for defaults, interactions, persistence, and verification.

## Workspace execution limits

Open **Execution limits** to configure aggregate process concurrency, shared host concurrency, and shared host delay. Defaults are host CPU count × 16, 2, and 100ms. Per-crawl settings can impose stricter bounds. Applying lower limits blocks new admissions until existing work falls below them; in-flight requests finish normally.

`POST /workspace/settings` with `{}` reads revision and execution settings. An update supplies `revision` and `execution`. Stale revisions fail. Registry migration imports the smallest old process/host ceilings and longest delay and writes an exact private pre-migration backup under `migration-backups`, outside Git. Original historical snapshots are retained.

Configuration updates retain the run ID, frontier, pending discoveries, retries, queues, results, counters, and active elapsed time. Applying changes checkpoints workers before reopening the same storage. The original configuration snapshot and earlier historical runs remain recorded. Custom pipelines retain opaque parameters; existing queue/frontier storage identities cannot be replaced by a configuration edit.

Per-host concurrency accepts integers from 1 through 8192, default 2. The effective limit is the stricter of the crawl and workspace shared host ceilings.

### Workspace configuration and extension exclusions

Shared process concurrency, host concurrency, and host delay are configured under
**Workspace configuration**. Crawl editors retain local limits and display the
shared host ceiling. Fixed fetch mode hides controls used only by adaptive mode.

**URL filters → Excluded file extensions** accepts comma-separated suffixes with
optional leading dots, for example `zip, .PDF, tar.gz`. Matching ignores case,
query strings, and fragments and uses the URL's decoded filename. `.zip` matches
`archive.ZIP?download=1`, but does not match `archive.zip.html`. Empty input disables
extension exclusions. Custom pipelines use `config.http_client.excluded_extensions`
as an array; frontier resources may additionally declare local admission exclusions. Global
HTTP exclusions are inherited at execution rather than copied into saved resource
parameters.
Robots-policy retrieval is auxiliary traffic and remains available when `.txt`
is excluded; its proxy and hostname restrictions still apply.
Exclusions apply to crawl documents before network access, including redirects and previously queued
requests. Excluded records count as policy skips, not fetch failures or successful
visits. Removing a rule permits subsequent rediscovery of those URLs.

Frontier selection continues through bounded index windows when leading records
are cooling. Fetch metrics include the selection reason and inspected, cooling,
eligible, and published counts. BFS/DFS priority applies to eligible work; a cooled
prefix does not prevent other destinations from being scheduled.

### Interactive network

Use **Graph** in the shared Crawl / Reports / Graph navigation, or **Reports → Interactive network**, to open the server-generated whole-network explorer. Generation, resource limits, zoom tiers, node metrics, and validation commands are documented in [Generated host network](host-network.md).

Runtime storage recovery has a separate startup deadline, defaulting to five minutes. Set `-startup-timeout 10m` when starting the application to change it. This process setting does not change crawl definitions or the shutdown deadline.

Protection configuration, exclusion and purge behavior: [Crawler protection](crawler-protection.md).
