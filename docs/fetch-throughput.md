# Fetch throughput

New crawls start in **fixed** mode with `runtime.NumCPU() * 4` fetch workers (32 on an eight-CPU machine). This is distinct from **Parallel requests per site**, which defaults to two. The settings appear under **Fetch throughput** in both the guided crawl editor and the expert editor. Every setting below is editable in the UI; none requires hand-editing JSON.

## Execution within the worker runtime

The ordinary HTTP worker manager still owns workers and transactions. Optional queue admission acquires execution capacity **before** a crawl/file queue leases its next message, or a channel queue hands off its message. The permit is released when the delivery transaction ends. Waiting for an adaptive or process slot therefore does not consume a durable delivery attempt. Cancellation and shutdown release permits and cancel pending admission.

Each crawl has its own initial concurrency, adaptive bounds, queues, frontier, results, and saved counters. A FIFO process broker shares capacity among eligible waiting crawls. Workspace **Execution limits** sets the process ceiling once, defaulting to host CPU count × 16. Reducing a ceiling lets already admitted requests finish; it prevents additional admission until usage drops below the new ceiling. The same rule applies to adaptive reductions. Thus active requests can temporarily exceed a newly reduced effective limit.

Workspace **Execution limits** also sets shared host concurrency and delay, defaulting to two and 100ms. Per-crawl host limits/delays also apply; the stricter combination controls requests. Robots, feeds, and redirects share host pacing and HTTP timeout configuration. The total execution ceiling measures page-fetch deliveries, including their policy checks and object writes; feed jobs are separate worker jobs and are not counted as page-fetch attempts.

Legacy definitions with no `fetch` object retain four guided fetch workers. Expert pipelines retain their explicit worker counts; omitted counts retain the existing runtime default. They still participate in process admission. **Configure fetch throughput** opts an older configuration into the new controls. In an expert pipeline, **Apply fetch settings and worker count** explicitly updates built-in HTTP worker counts, queue generation support, and frontier retry policy while preserving other component parameters and large JSON integers. Saving and editing are locked until Apply completes. Adaptive worker pools have the configured maximum number of ordinary workers; admission selects how many may execute. Custom inbox implementations must provide the optional `SetDispatchGate` hook to use these execution controls.

## Retries and recovery

For built-in durable frontiers, **Retries per request**, **Initial retry delay**, and **Maximum retry delay** configure frontier retries instead of inline HTTP sleeps. Delivery failures and network retries have different purposes: a received HTTP outcome commits one durable retry decision, with a new request ID and queue generation for each attempt. Replayed outcomes cannot create duplicate retries. Pipelines without a durable retry-capable frontier retain inline HTTP retries.

Retryable failures include timeouts, connection/body-read failures, HTTP 429, and HTTP 5xx. Policy denials, cancellation, response-size rejection, and ordinary HTTP 4xx do not schedule retries. Backoff doubles to the configured maximum. A server's `Retry-After` can extend that delay beyond the maximum backoff. Exhausted retries become terminal failures. A request budget counts every dispatch, including retries, and can end a run before its retry allowance is exhausted.

After **Failures before host cooldown**, failed hosts enter an exponentially increasing cooldown bounded by **Maximum host cooldown**. Once eligible, **Recovery probe spacing** prevents a burst of probes. A successful response clears the failure streak. **Retry scheduling share** reserves recovery opportunities among fresh candidates; unavailable classes allow the other class to proceed. Fresh and retry candidates use separate indexes, so either class stays visible behind a large backlog of the other. An upgrade builds the fresh indexes from active requests, without scanning completed URL history; an interrupted rebuild can safely resume on restart. Durable retry-wait records remain pending and are protected from terminal-record expiration. They survive shutdown/restart and prevent premature queue-drained completion. Retry indexes and traversal indexes are updated atomically.

## Timeouts and dispatch

**Request timeout** is the overall network budget for one attempt, shared across redirects. Host pacing waits are outside that budget. Connection, TLS handshake, response header, and response body timeouts can be set separately. `inherit` or `0s` leaves a phase bounded by the remaining overall budget. The connect timeout includes the SOCKS connection and handshake. A body timeout starts when response headers arrive. Policy/robots processing and object-storage work also contribute to measured worker attempt latency.

The publisher refills below **Fetch refill low watermark multiplier × effective concurrency**, targeting **Fetch queue target multiplier × effective concurrency**. **Fetch queue hard ceiling** and **Maximum refill batch** bound each refill. **Candidate selection window** bounds the candidate set. Existing work above a newly reduced target drains naturally. These controls work through the existing frontier publisher job and durable queue, rather than a separate crawl loop.

Downstream queue and memory watermarks pause new admission and publication, then resume below the configured low watermarks. Memory pressure includes Go heap usage and anonymous response mappings. Low disk space on configured storage/output paths and queue/storage health errors also pause admission. Sampling is periodic, so already admitted work may finish and enqueue results after a watermark is crossed. These are backpressure thresholds, not hard limits on RSS or disk usage. The memory default is one quarter of detected physical/cgroup memory, capped at 4096 MiB (minimum 64 MiB); its resume threshold is 75% of that value. The free-disk default is 64 MiB. Configure these for the number and size of stores you intend to run.

## Adaptive control

Adaptive mode starts at **Concurrent requests overall** and stays within configured minimum/maximum bounds. **Adjustment interval** defaults to 30s; a window expires even without completed requests. CPU pressure or a sampled local/proxy overload ratio can reduce concurrency by the configured multiplier. The minimum sample count applies only to the overload ratio. Destination latency and previous throughput gains do not control global reductions.

Memory, downstream queues, disk, and storage-health gates block admission independently. Blocking alone does not shrink concurrency. Gates clear using their own health checks and hysteresis. Remote failures remain subject to host retries and cooldowns.

With clear pressure, runnable demand, and used execution capacity, each eligible interval increases concurrency by the configured step, defaulting to host CPU count (minimum one). Growth requires neither successes nor completed samples. Host pacing and shared-capacity waiting are distinguished from network saturation using bounded dispatcher observations and broker waits; no full-frontier demand scan is required.

Old evaluation/cooldown settings migrate to their longer interval. Explicit increase steps remain unchanged. Retired latency/gain controls are removed from active configuration; historical snapshots retain the original values. Fixed mode retains admission gates with a fixed concurrency target. [`flow.Options`](../flow/policy.go) defines individual bounds and UI controls.

## Monitoring and persistence

### Protected response storage

On Linux, protected responses with a known, identity-encoded body of at least 64 KiB (within the configured response-size limit) are read into an anonymous memory-backed file. The normal HTTP transport still handles SOCKS, HTTP proxies, TLS, redirects, timeouts, and connection reuse. Every received chunk passes the same incremental protection inspection before publication; a match cancels the response without waiting for the remaining declared body. Mapped memory is included alongside Go heap usage in the memory admission gate.

After inspection, the filesystem object store computes the checksum and uses `splice` through a pipe to publish a private temporary object, followed by sync and atomic rename. The protection write barrier encloses publication. This avoids a body-sized Go allocation and the final userspace storage copy; it does not bypass the HTTP transport or make inspection zero-copy. Cancellation and failed writes remove temporary objects and release mappings and descriptors.

Small bodies, unknown lengths, gzip, chunked responses, unavailable memory mappings, and non-Linux systems retain buffered inspection. Unsupported kernel/filesystem splice operations fall back to buffered copying from the current offset without repeating the HTTP request or duplicating bytes. Object stores without the optional file-publication interface retain their ordinary `Put` path. These choices require no configuration or state migration.

Fetch counters report successfully inspected `body_mapped` bodies, `body_buffered` selections, actual `object_splice_bytes`, and successfully published `object_buffered_bytes`. Splice bytes can include a partial failed write; they are not successful-response totals. The text normalizer collapses whitespace without allocating a slice of every word and checks ASCII whitespace directly, retaining Unicode classification for other characters. Tests compare its output with the previous implementation, including Unicode and invalid UTF-8.

The production-path benchmark is individually runnable on Linux:

```sh
go test ./worker/fetcher -run '^$' -bench '^BenchmarkProtectedFetchBody$' -benchtime=200x -count=5 -benchmem
```

**Fetch metrics** reports page-attempt, successful-response, HTML-response, unique-HTML, admitted-URL, finalized-request, and byte rates; active/effective/process capacity; retry/cooldown counters; response statuses and classified failures; received/stored bytes; heap/CPU/disk; queue depth, oldest age and growth; storage conflicts/failures; and bounded latency summaries for execution wait, origin wait, connect, TLS, headers, body, storage, and frontier storage. Header latency includes connect/TLS time. Body/storage measurements can overlap because object storage streams the body.

Successful response rates count HTTP 2xx/3xx fetches; HTML response rate does not imply unique content. Unique HTML is established by the durable frontier after analysis. Rates use the configured window, starting with elapsed session time; latency summaries use at most **Maximum samples per metric** recent observations, while their sample count is cumulative for that process session. No per-URL metric series are retained.

Cumulative attempt/error/status/byte/storage counters are atomically checkpointed beside runtime state at **Metrics save interval**, and on orderly shutdown. The file is private to the run and removed with crawl data. An abrupt process failure can lose counters since the last checkpoint; durable frontier state remains authoritative for retries and finalized work. Recent rate windows, latency samples, and adaptive learning restart with the process. Saved run summaries remain available in run history. Metrics persistence errors appear in the adjustment/status text.

## Validation

`make test` runs backend and frontend suites; `make test-race` runs backend race checks. `make test-ui` runs real-backend browser scenarios and requires complete coverage of the checked-in targeted inventory within 30 minutes. See [browser validation](../web/README.md).

`TestMappedLargeResponsesAndLimitRecovery` exercises 32 MiB binary and 8 MiB HTML transfers, rejection at the configured size limit, and subsequent worker progress. `TestFilePublicationDiskFull` requires `DELVER_OBJECTSTORE_FULL_DIR` to name a disposable tmpfs of at most 4 MiB; run it explicitly to verify real ENOSPC cleanup and recovery. Ordinary unit tests also inject unsupported and partially completed splice operations, interrupted calls, cancellation, and write errors.

`make test-benchmark BASELINE=/private/path/baseline.log BASELINE_APP=/private/path/pipeline-baseline.log` compares five final repetitions against five same-host baseline repetitions at 4, 16, 32, and 64 slots. Both isolated network capacity and complete worker-pipeline median successful throughput may not regress by more than 10%. Raw attempts, successes, latency, heap, and peak concurrency are emitted by `BenchmarkFetchCapacity`; isolated application tests cover pipeline progress, retries, and adaptive recovery.

`make test-soak`, `make test-million`, `make test-million-disk`, `make test-elasticsearch`, and `make test-tor` are separate acceptance gates. They require their documented prerequisites and retain the full endurance/scale/live-network workloads. Generated reports belong outside Git. Prior measurements in [endurance validation](endurance-validation.md) describe earlier code and do not establish current-code acceptance.

## Resource settings and measurements

**Request queue cache (MiB)** is a crawl setting, default 64, range 16–1024.
Guided definitions use `request_queue_cache_mb`; expert crawl-queue resources use
`parameters.cache_budget_mb`. Expert **Apply queue cache** patches the named
resources through the server and preserves other JSON parameters. Missing values
resolve to 64 during preparation. Historical snapshots and existing database
files are retained. Cache capacity is distinct from process heap/RSS: memtables,
worker state, response bodies, database bookkeeping and filesystem mappings also
consume memory. Synchronous writes and supported transaction sizes are retained.

Fetch metric details include inspection reservations, peak reserved bytes,
waiters and reservation-wait latency. Resource metrics report request-cache
capacity and file-queue directory scans/cached candidates. The file queue keeps
at most 256 candidate names; queue files remain authoritative. Enqueues wake the
consumer, and periodic directory reconciliation retains filesystem recovery.

Eligible mapped, known-length protected responses reserve twice their bounded
length plus an 8 MiB inspection allowance, capped at the previous worst-case
reservation. Unknown, decompressed, encoded and buffered responses keep the
worst-case reservation. Reservations are acquired atomically and held through
publication. The inspection budget accounts for reservation capacity, not all
process memory or race-detector overhead.

Compiled Go benchmarks can be compared locally using:

```sh
go test -c -o /private/path/candidate.test ./queue
make test-efficiency-benchmark \
  BASELINE_TEST_BIN=/private/path/baseline.test \
  CANDIDATE_TEST_BIN=/private/path/candidate.test \
  EFFICIENCY_BENCHMARK='BenchmarkRequestQueueResources' \
  BENCHTIME=1x VALIDATION_ROOT=/private/path/evidence
```

The runner takes two unchanged controls and five alternating pairs, records
binary hashes, CPU allocation, host load, allocations, process CPU, peak RSS,
descriptors and disk writes, and returns a nonzero inconclusive result when
timing CV or control drift exceeds 10%. Process samples include fixture setup;
Go benchmark measurements exclude setup where their implementation stops the
timer. The runner does not accept a remote host and does not initiate Tor
traffic. Workloads use synthetic data and local fixtures. Run builds and test
suites separately from measurements. `--storage-root /dev/shm` selects a tmpfs
comparison; persistent-disk durability and scale gates remain separate.

Individual workloads are selectable through normal `go test -bench` commands:
`BenchmarkProtectionEfficiency`, `BenchmarkProtectedResponseSizes`,
`BenchmarkInspectionAdmission`, `BenchmarkStagingTransfer`,
`BenchmarkStagedDeduplication`, `BenchmarkFileQueueSelection`,
`BenchmarkRequestQueueResources`, `BenchmarkRequestQueueCacheWorkingSet`,
`BenchmarkSharedPageAnalysis`, `BenchmarkWorkspaceStates`, and
`BenchmarkReportResponseWrites`.
Production-build allocation allowance is checked by
`TestInspectionTransientAllowance`; race builds retain reservation, cancellation,
misleading-length, decompression and fallback correctness checks separately.
