# Fetch throughput

New crawls start in **fixed** mode with `runtime.NumCPU() * 4` fetch workers (32 on an eight-CPU machine). This is distinct from **Parallel requests per site**, which defaults to two. The settings appear under **Fetch throughput** in both the guided crawl editor and the expert editor. Every setting below is editable in the UI; none requires hand-editing JSON.

## Execution within the worker runtime

The ordinary HTTP worker manager still owns workers and transactions. Optional queue admission acquires execution capacity **before** a crawl/file queue leases its next message, or a channel queue hands off its message. The permit is released when the delivery transaction ends. Waiting for an adaptive or process slot therefore does not consume a durable delivery attempt. Cancellation and shutdown release permits and cancel pending admission.

Each crawl has its own initial concurrency, adaptive bounds, queues, frontier, results, and saved counters. A FIFO process broker shares capacity among eligible waiting crawls. The **smallest process request ceiling among active crawls** applies across them. Reducing a ceiling lets already admitted requests finish; it prevents additional admission until usage drops below the new ceiling. The same rule applies to adaptive reductions. Thus active requests can temporarily exceed a newly reduced effective limit.

Shared host limits use the smallest active **Shared requests per host** and the largest **Shared delay per host**. Defaults remain two and 100ms. Per-crawl host limits/delays also apply; the stricter combination controls requests. Robots, feeds, and redirects share host pacing and HTTP timeout configuration. The total execution ceiling measures page-fetch deliveries, including their policy checks and object writes; feed jobs are separate worker jobs and are not counted as page-fetch attempts.

Legacy definitions with no `fetch` object retain four guided fetch workers. Expert pipelines retain their explicit worker counts; omitted counts retain the existing runtime default. They still participate in process admission. **Configure fetch throughput** opts an older configuration into the new controls. In an expert pipeline, **Apply fetch settings and worker count** explicitly updates built-in HTTP worker counts, queue generation support, and frontier retry policy while preserving other component parameters and large JSON integers. Saving and editing are locked until Apply completes. Adaptive worker pools have the configured maximum number of ordinary workers; admission selects how many may execute. Custom inbox implementations must provide the optional `SetDispatchGate` hook to use these execution controls.

## Retries and recovery

For built-in durable frontiers, **Retries per request**, **Initial retry delay**, and **Maximum retry delay** configure frontier retries instead of inline HTTP sleeps. Delivery failures and network retries have different purposes: a received HTTP outcome commits one durable retry decision, with a new request ID and queue generation for each attempt. Replayed outcomes cannot create duplicate retries. Pipelines without a durable retry-capable frontier retain inline HTTP retries.

Retryable failures include timeouts, connection/body-read failures, HTTP 429, and HTTP 5xx. Policy denials, cancellation, response-size rejection, and ordinary HTTP 4xx do not schedule retries. Backoff doubles to the configured maximum. A server's `Retry-After` can extend that delay beyond the maximum backoff. Exhausted retries become terminal failures. A request budget counts every dispatch, including retries, and can end a run before its retry allowance is exhausted.

After **Failures before host cooldown**, failed hosts enter an exponentially increasing cooldown bounded by **Maximum host cooldown**. Once eligible, **Recovery probe spacing** prevents a burst of probes. A successful response clears the failure streak. **Retry scheduling share** reserves recovery opportunities among fresh candidates; unavailable classes allow the other class to proceed. Fresh and retry candidates use separate indexes, so either class stays visible behind a large backlog of the other. An upgrade builds the fresh indexes from active requests, without scanning completed URL history; an interrupted rebuild can safely resume on restart. Durable retry-wait records remain pending and are protected from terminal-record expiration. They survive shutdown/restart and prevent premature queue-drained completion. Retry indexes and traversal indexes are updated atomically.

## Timeouts and dispatch

**Request timeout** is the overall network budget for one attempt, shared across redirects. Host pacing waits are outside that budget. Connection, TLS handshake, response header, and response body timeouts can be set separately. `inherit` or `0s` leaves a phase bounded by the remaining overall budget. The connect timeout includes the SOCKS connection and handshake. A body timeout starts when response headers arrive. Policy/robots processing and object-storage work also contribute to measured worker attempt latency.

The publisher refills below **Fetch refill low watermark multiplier × effective concurrency**, targeting **Fetch queue target multiplier × effective concurrency**. **Fetch queue hard ceiling** and **Maximum refill batch** bound each refill. **Candidate selection window** bounds the candidate set. Existing work above a newly reduced target drains naturally. These controls work through the existing frontier publisher job and durable queue, rather than a separate crawl loop.

Downstream queue and heap watermarks pause new admission and publication, then resume below the configured low watermarks. Low disk space on configured storage/output paths and queue/storage health errors also pause admission. Sampling is periodic, so already admitted work may finish and enqueue results after a watermark is crossed. These are backpressure thresholds, not hard limits on RSS or disk usage. The heap default is one quarter of detected physical/cgroup memory, capped at 4096 MiB (minimum 64 MiB); its resume threshold is 75% of that value. The free-disk default is 64 MiB. Configure these for the number and size of stores you intend to run.

## Adaptive control

Adaptive mode starts at **Concurrent requests overall** and stays within **Minimum concurrency** and **Maximum concurrency**. It requires queued demand, completed samples, an evaluation window, and an adjustment cooldown before changing capacity. Healthy throughput gains grow capacity by **Concurrency increase step**. Configured pressure, CPU saturation, proxy/local failures, or smoothed latency inflation reduce it by **Concurrency decrease multiplier**. Unreachable destination failures do not by themselves indicate global overload; host cooldowns handle them.

The UI exposes sampling/evaluation intervals, minimum samples, cooldown, growth/decrease parameters, latency smoothing/inflation, useful throughput gain, overload failure ratio, CPU thresholds, heap thresholds, disk minimum, and all dispatch/recovery limits. Bounds and cross-field constraints are checked before saving. The source of truth for individual limits is [`flow.Options`](../flow/policy.go), also used to generate the UI catalogue. Fixed mode retains resource backpressure while keeping its concurrency target fixed.

## Monitoring and persistence

**Fetch performance** reports page-attempt, successful-response, HTML-response, unique-HTML, admitted-URL, finalized-request, and byte rates; active/effective/process capacity; retry/cooldown counters; response statuses and classified failures; received/stored bytes; heap/CPU/disk; queue depth, oldest age and growth; storage conflicts/failures; and bounded latency summaries for execution wait, origin wait, connect, TLS, headers, body, storage, and frontier storage. Header latency includes connect/TLS time. Body/storage measurements can overlap because object storage streams the body.

Successful response rates count HTTP 2xx/3xx fetches; HTML response rate does not imply unique content. Unique HTML is established by the durable frontier after analysis. Rates use the configured window, starting with elapsed session time; latency summaries use at most **Maximum samples per metric** recent observations, while their sample count is cumulative for that process session. No per-URL metric series are retained.

Cumulative attempt/error/status/byte/storage counters are atomically checkpointed beside runtime state at **Metrics save interval**, and on orderly shutdown. The file is private to the run and removed with crawl data. An abrupt process failure can lose counters since the last checkpoint; durable frontier state remains authoritative for retries and finalized work. Recent rate windows, latency samples, and adaptive learning restart with the process. Saved run summaries remain available in run history. Metrics persistence errors appear in the adjustment/status text.

## Validation and reproducible capacity comparison

Run:

```sh
go test ./...
go test -race ./...
go vet ./...
go test ./util -run '^$' -bench BenchmarkFetchCapacity -benchtime=1x -count=1
cd web
npm test
npm run build
node ../scripts/package-ui.mjs
npm run test:browser -- --workers=4
```

The controlled benchmark sends 256 requests through a local HTTP proxy to distinct synthetic hostnames: 75% return HTML after 20ms, 12.5% return HTTP 503 after 60ms, and 12.5% remain offline until the configured 250ms timeout. It measures network/admission capacity, not extraction/sink throughput or real Tor latency. One eight-CPU run produced:

| Slots | Attempts/s | Successful pages/s | p95 attempt ms | Peak requests | Heap MiB |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 4 | 73 | 55 | 251 | 4 | 2.9 |
| 16 | 256 | 192 | 251 | 16 | 2.6 |
| 32 | 424 | 318 | 251 | 32 | 2.6 |
| 64 | 685 | 514 | 251 | 64 | 3.7 |

The application integration test separately exercises full-pipeline durable retry and completion. UI tests cover all 864 combinations of mode × strategy × source × connection × robots × scope × completion behavior, plus nondefault round trips for every setting and 48 live combinations of mode × connection × robots × timeout inheritance × retry allowance. Numeric/duration boundaries, cross-field constraints, migration, lifecycle, expert editing, and existing workspace regressions are separate checks. Continuous numeric values and arbitrary custom plugins cannot be exhaustively enumerated.

Verification completed on November 15, 2026 (Linux/Chromium): the 1,302-case full browser suite passed, followed by a 62-case focused run covering the latest runtime/editor changes and four additional mobile/accessibility and pressure-recovery cases. All 11 existing expert/action-recovery regressions passed again after locking Save during an outstanding Apply request. All 83 frontend unit cases, the full Go test suite, the full Go race suite, `go vet`, the production Go/UI builds, catalogue synchronization, and whitespace checks passed. Manual UI checks covered validation, configuration persistence, initiation, metrics, pause/resume/stop, and expert Apply/save using isolated local fixtures. These checks do not measure a live Tor network's capacity.

The subsequent [final-code endurance and scale validation](endurance-validation.md) records the fresh 1,306-case browser rerun, uncached backend suites, sustained race-enabled crawl soak, and million-URL lifecycle checks, including defects found and corrected during those runs.
