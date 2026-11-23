# Frontier workers

The guided workspace configures frontier workers without a scenario file. Synthetic pipeline examples are in `application/testdata/examples`. Run `go run ./examples/site`, start `go run .`, and create a crawl using `http://localhost:8088/`. Scope and hostname rules apply to seeds, discoveries, feeds, and redirects.

## Pipeline

Existing seed publisher → crawl queue → HTTP fetcher → composite extractor → frontier accumulator → shared frontier store. A frontier publisher job replenishes the crawl queue from the store; a cleanup job expires history. Workers use the normal worker/job managers and inherit pause, resume, cancellation, and shutdown. They do not run a separate runtime. The accumulator must consume every analysis outcome for frontier requests.

Configure `frontier_store` as a resource with a unique `path`. Inject it into each worker with `parameters.frontier_store`. The publisher additionally requires `parameters.output_queue` referencing a durable crawl queue. Publisher and cleanup workers require `manager: "job"`, a positive `interval`, and should have one instance. Use `100ms` for publishing and `1m` for cleanup; cleanup enforces its own configured interval as well.

The crawl queue must enable `notify_dispatch` to remove polling latency and `accept_frontier_generations` to accept rediscovery after frontier deduplication expires. Generations never independently replace pending or leased work. Page bodies stay in the existing object store; the crawl queue retains delivery, retries, staged outputs, and failed-record recovery.

## Traversal

Publisher `strategy` accepts `breadth_first`, `depth_first`, `host_diverse` (default), or `adaptive`. Dedicated scheduling indexes avoid reading completed history. `window` bounds candidate reads (default 1024), `batch_size` bounds output (32), and `low_water` controls replenishment (64). Host strategies read one candidate per origin into each window and rotate the starting origin between batches. Adaptive selection uses a moving average of distinct successful HTML per fetch millisecond, with every fifth selection reserved for host exploration. Selection is approximate within a bounded window, not a global ranking across every origin.

Accumulator `scope` accepts `same_origin`, `same_domain` (default), or `open_web`. Defaults are `max_depth: 10` within a registrable domain and `max_hops: 3` across domains. Crossing domains resets depth. `allow_pattern` and `deny_pattern` are optional URL regex filters. HTTP/HTTPS URLs are normalized before deduplication; query parameters are preserved.

Go integrations can inject `frontier.Selector`, accumulator `Filters`, or a store `RewardPolicy`. The `frontier.FrontierStore` interface separates workers from storage. The shipped backend uses Badger, matching existing queue storage. SQLite is included as a benchmark comparison, not as a shipping backend.

## Capacity and retention

Resource defaults: `cache_budget_mb: 64`, `max_pending: 100000`, `max_pending_per_origin: 10000`, `min_free_bytes: 67108864`, and `origin_delay: "100ms"`. Pending counts include dispatched work awaiting analysis. Cache budgets bound configured caches and memtables, not total process RSS. HTTP limits remain authoritative. Optional `max_urls`, `max_urls_per_origin`, and `pattern_budgets` (regex-to-count map) impose lifetime admission budgets; omitted budgets are unlimited. Budget rejection skips the link. Soft capacity pressure transfers links into durable staged discoveries. The publisher automatically retries admission as active capacity becomes available, without consuming analysis-input retries. Chunk markers make replay idempotent; unfinished chunks never expire. Staged links remain protected work, are reported separately from the active pending limit, and use disk space until admitted or explicitly rejected by a configured crawl budget. Disk-headroom or actual storage failures remain visible failures; they are not treated as soft capacity pressure.

```json
"retention": {
  "completed_for": "7d",
  "failed_for": "30d",
  "dedup_for": "90d",
  "cleanup_interval": "5m",
  "cleanup_batch_size": 1000
}
```

Each history duration accepts `"never"`. Omitted fields use defaults. Deduplication retention must cover both detail periods. Set only `dedup_for` to `"never"` to reclaim detailed history while retaining permanent URL identities; set all three to `"never"` to retain full records indefinitely.

Retention starts at terminal outcome. Details compact into URL identity records before final expiry. Pending work and unfinished reservations never expire automatically. Cleanup uses an expiration index and bounded transactions, removes associated records atomically, and attempts value-log garbage collection. Disk space reclamation is asynchronous and is not guaranteed to immediately match logical deletion.

Ordered-key seeking resumes strictly after the cursor, even when its record has been deleted. Scheduler windows are refreshed each job invocation so new candidates behind a previous cursor remain visible.

Retention controls frontier history, not bodies, extraction queues, crawl-queue history, or lifetime budget counters. Permanent deduplication necessarily grows with distinct URLs. This is a single-process design; it has no established multi-machine or maximum-size operating guarantee. Storage health and pending counts appear through existing runtime diagnostics.

## Verification

Run `go test ./frontier ./queue ./worker/frontierworker ./application`. The measured workload and scale results, reproducible commands, and operating envelope are in [frontier-benchmarks.md](frontier-benchmarks.md).

Extension exclusions are applied before discovery staging, including seed admission. Each 128-input chunk atomically stores allowed links, its replay marker, and an aggregate policy-skip count. Replaying that chunk does not repeat its skips. Already-staged links excluded by a new policy are removed and counted in the same batched acknowledgment transaction. Admission and HTTP checks remain authoritative for queued requests and redirects. A new discovery after removing a rule can admit a previously excluded URL; replaying an already committed input retains its original decision.

For a controlled filtering comparison, capture five baseline repetitions of `BenchmarkDiscoveryExtensionBatch` and `BenchmarkWorkspaceDiscoveryPressure` with `go test -run '^$' -bench NAME -benchtime=1x -count=5` in their respective packages. Set `TMPDIR` to persistent storage for the frontier benchmark. Run `make test-discovery-benchmark DISCOVERY_BASELINE=/private/frontier-baseline.log DISCOVERY_PIPELINE_BASELINE=/private/pipeline-baseline.log VALIDATION_ROOT=/private/results` against the updated code. The target records timings, database commits, policy skips, analyzed-queue peaks, and sampled fetch-idle time, and rejects an allowed-only median slowdown above 10%.

## Staged discovery and admission

Discovery events own their durable staging records and replay receipts.
Admission normalizes URLs, deduplicates frontier identities and retains better
traversal paths. Pending discoveries remain durable until admitted or excluded.

Unconstrained admission windows commit up to 128 records together. A rejected
batch rolls back completely before the ordinary per-record path handles that
window. Oversized batches fall back to individual admissions; acknowledgments
remain after commit. Report response writes coalesce callers already waiting,
with a maximum of 128 and no batching timer. Uncontended report writes execute
in the caller. Object and file-queue file syncs remain enabled.
