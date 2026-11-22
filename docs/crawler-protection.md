# Crawler protection

Workspace configuration contains **Blocklists** and **Restricted content**. Crawl configuration contains **Trap detection** and **Restricted-content purge depth** under URL filters. Monitoring and reports do not edit workspace policy.

## Site identity

Scheduling credit and site admission budgets use the registrable domain from the Public Suffix List, the underlying onion address, or the exact IP address. Subdomains share this identity. HTTP origins, robots rules, pacing, connections and credentials remain origin-specific. URLs retain their individual records and traversal depth.

Existing frontier indexes and site counters migrate in bounded, replay-safe batches at startup. Request IDs, generations, retries and seek order remain intact.

## Trap detection

HTML tag paths and ancestry shingles produce text-independent structural fingerprints. MinHash and a bounded locality-sensitive index identify related structures without comparing every pair. Text, attribute values, attribute order and sibling section order do not identify a family. Versioned hash-vector encoding preserves all 64 bits through JSON worker deliveries and Elasticsearch; earlier array encodings remain readable.

Automatic exclusion requires evidence across at least three canonical sites, repeated link/discovery patterns, and two consecutive confirmation windows with low discovery novelty. Cross-site link fraction and fresh-link fraction distinguish replicated expansion from ordinary internal navigation and stable shared navigation. Enclosure or a shared template cannot independently exclude a site. A static decorative outward link does not reset novelty indefinitely.

New crawl defaults:

| Control | Default |
| --- | --- |
| Site observation window | 20 pages |
| Family observation window | 100 pages |
| Structure similarity | 0.70 |
| Link pattern redundancy | 0.80 |
| Maximum discovery novelty | 0.05 |
| Minimum fresh-link fraction | 0.50 |
| Minimum cross-site link fraction | 0.50 |
| Samples per confirmation window | 5 |
| Minimum links per page | 8 |

Every control is editable in the crawl UI. **Detect crawler traps** records measurements; **Automatically exclude detected traps** additionally excludes qualifying sites. Existing configurations without trap settings retain that omission until configured. Numerical defaults are checked against `trap/testdata/calibrated-defaults.json`; calibration documents and held-out evaluation networks are separate fixtures.

Exclusions and observations persist in frontier storage. Excluded pending requests move outside active frontier capacity; they remain available for explicit **Restore** under **Trap exclusions**. Changing detection settings does not silently restore an existing exclusion. Restoration does not override workspace blocklists or restricted-content quarantines.

The acceptance corpus measures these heuristics; it does not establish perfect classification of arbitrary sites. A single-site trap cannot satisfy the three-site automatic-exclusion condition. Site budgets and shared scheduling credit still constrain subdomain multiplication.

## Blocklists

Lists accept plaintext domain names or complete HTTP(S) URLs, one per line. Blank lines and `#` comments are ignored. A domain entry covers that domain and its subdomains; a URL entry keeps URL scope. Invalid entries identify their line number.

**Preview list** reports additions, removals and matching stored report records. **Activate and purge** confirms destructive activation. Scheduled feeds default to six-hour refresh intervals; downloaded data is validated before replacing the active copy. An unchanged download does not enqueue another purge. A failed refresh keeps the last successful copy and records its error. A never-loaded feed can be saved inactive and retried with **Refresh**.

Disabling or deleting a list removes that source and cancels its unfinished purge. Replacing a source cancels its obsolete purge and creates the replacement operation. Previously deleted data is not reconstructed. Independent lists and restricted-content quarantines remain effective. Repeating an accepted activation does not enqueue a duplicate purge.

The optional [HaGeZi threat feed](https://github.com/hagezi/dns-blocklists) supplies threat-intelligence filtering; it is not CSAM coverage. The [IWF URL List](https://www.iwf.org.uk/our-technology/our-services/url-list/) is available to members under licence. Compatible operator-supplied plaintext HTTP(S) feeds remain supported.

## Restricted-content rules

Rules contain contextual phrases, one per line. Conservative starter rules cover explicit solicitation phrases. `CP` and `CPAN` alone do not trigger exclusion. Matches represent suspected indicators, not criminal-content determinations.

Known URL/domain blocks are checked before connecting, including redirects and robots requests. Response text is inspected locally in bounded buffers before storing response objects or emitting extracted results. Classification does not download media or call OpenAI. Content-only detection necessarily receives response bytes before it can decide.

The inspection memory budget is configurable in Workspace configuration. Response-size limits must fit the budget; incompatible settings are rejected. A protection persistence failure blocks admission and writes until the pending deny is durable. It does not acknowledge unrelated deliveries as blocked URLs.

## Propagation and cleanup

**Restricted-content purge depth** defaults to **0**:

| Depth | Selected sites |
| --- | --- |
| 0 | Detected canonical site |
| 1 | Also sites with recorded links into the detected site |
| 2 | Also sites linking into those referrers |
| Higher | Continue following incoming links |

The detecting crawl's recorded graph supplies the target set. Traversal uses shortest incoming-link distance, deduplicates sites and terminates cycles. Outgoing neighbors are not selected merely by association. The graph revision, coverage status, selected sites, distances and reasons are persisted before graph deletion. Retries and restart use that fixed set. Sites selected through propagation are labelled **Blocked by purge depth**.

Blocks apply workspace-wide. Matching active requests are cancelled immediately. Queues, workers and sinks recheck policy at write boundaries. Cleanup waits for writes admitted before activation, then verifies queues, frontier records, results and report storage before completing. Physical result files are rewritten atomically. Search cleanup preserves other crawls and runs, and removes matching URL references from retained documents without retrieving page text or media.

Purge progress records each storage destination. Failed cleanup remains visible and supports **Retry purge**. Interrupted workspace shutdown preserves pending cleanup for restart. Existing crawl configuration, unrelated queues, results and run identity remain intact. Deletion cannot remove the detecting graph before propagation targets have been captured.

Cleanup covers managed frontier/staging/retry records, durable deliveries and output caches, response objects and temporary files, JSONL results, supported Elasticsearch documents, graph records and generated snapshots, source declarations, application log buffers, legacy journals and private migration backups. Historical configuration edits preserve unknown fields, large integers and credentials. Old snapshot/tile requests are rejected during cleanup and invalidated afterward.

Completion verifies removal through managed file, database and search interfaces. It does not assert forensic erasure of freed database storage, SSD blocks or old Go heap copies, nor deletion of externally managed logs or offline client downloads.

Incident export contains metadata only: location, time, source/rule, disposition, purge depth, graph coverage/revision, selected sites and cleanup status. It excludes bodies, excerpts, screenshots and media. Ordinary threat-list matches are labelled **Blocklist match**. Delver does not submit reports to authorities. [NCMEC's authenticated reporting API](https://report.cybertip.org/ispws/documentation/index.html) requires credentials supplied by NCMEC and is outside this implementation.

## Custom constructors

Existing worker/resource registration functions accept either `ProtectionMetadataOnly` for components that do not retain crawl data, or `ProtectionSupport{Configuration: cleaner}`. The latter requires the constructed component to implement `ProtectionParticipant`:

```go
SetProtection(*protection.Policy)
PurgeMatching(context.Context, func(string) bool) error
VerifyNoMatches(context.Context, func(string) bool) error
```

The pure configuration cleaner removes declared crawl sources while preserving credentials and opaque parameters. Runtime dependencies supply the same workspace policy. Custom network requests and final writes must honor that policy. Constructors with undeclared capabilities, incomplete runtime support or unsupported output destinations fail before execution. Cleanup also constructs supported historical components without running their workers. Close failures prevent completion. Unattributed legacy Elasticsearch logs require ownership migration; cleanup reports that limitation instead of claiming those documents were removed.

## Validation

All tests are selectable through Go, Vitest or Playwright. Focused commands:

```sh
go test ./protection ./trap ./site
go test ./frontier -run 'Trap|SiteMigration|Subdomains|PurgeFrontier'
go test -race ./application -run 'Purge|Protection|CustomResourceCleanup'
npm --prefix web test
npm --prefix web run test:browser -- workspace-protection.spec.js workspace-expert.spec.js
```

`make test`, `make test-race`, `make test-static`, `make test-ui`, `make test-million`, `make test-million-disk`, `make test-soak` and `make test-elasticsearch ES_URL=...` retain their normal entry points. The million-URL workloads include canonical-site purging, blocked rediscovery and seek continuity. The one-hour soak includes simultaneous fixed/adaptive crawls, network/storage pressure, a failed purge and retry, three restarts, checkpoint comparisons and shutdown checks.

The disk-full recovery test requires an isolated filesystem no larger than 16 MiB; it refuses larger filesystems. Run it with `DELVER_PROTECTION_FULL_DIR=/path/to/private/tiny-tmpfs go test ./protection -run '^TestProtectionRealDiskFullRecovery$' -count=1`. Public feed format validation is selectable with `DELVER_PROTECTION_FEED_FILE=/path/outside-git/feed.txt go test ./protection -run '^TestPublicThreatFeedFormat$' -count=1`; this parses the feed without visiting listed destinations.

Workspace-wide destructive browser tests use a separate real backend workspace so they cannot purge parallel lifecycle or graph fixtures. The coverage inventory includes every required browser case and rejects skipped, missing, duplicate or failed results.

For the isolated five-repetition binary comparison:

```sh
python3 scripts/benchmark-protection.py \
  --baseline /absolute/path/to/baseline-delver \
  --final /absolute/path/to/delver \
  --output /absolute/path/outside-git \
  --storage-root /dev/shm/delver-protection-benchmark
```

The controlled capacity comparison uses tmpfs, one seed discovering 4,096 independent HTML pages, fixed concurrency 32, 20 ms destination delay, and four status reads per second. It records configurations, binary hashes, throughput, runtime snapshots, network timings and process memory. Persistent-disk lifecycle and durability are verified separately; host-disk flush variability must not be labelled a measured code regression without a stable identical-binary control. A final median successful-throughput regression above 10% fails. Run it without competing heavy tests. Generated evidence, downloaded feeds, private configuration, incident exports and runtime data remain outside Git.
