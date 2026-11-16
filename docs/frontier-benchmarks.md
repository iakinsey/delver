# Frontier validation and operating envelope

Measured locally on an Intel i7-1185G7, Linux amd64, Go 1.27.4, with temporary databases on tmpfs. Synchronous durability options were enabled for both engines. These measurements compare application and storage work; tmpfs does not reproduce persistent-disk flush latency, power-loss behavior, network latency, or cold OS caches.

The tables below retain the earlier baseline measurements. The [final-code validation report](endurance-validation.md) records subsequent reruns after storage tuning, including the 1 GiB storage test and million-URL shipping-frontier workloads.

## Backend comparison

The same key/value prototype runs admission with scheduling indexes, ordered selection, reservation, publication confirmation, outcome updates, detail compaction, final deletion, and reopening with a stable reservation identity. This compares equivalent transactional operations, not a production SQLite frontier implementation.

At 1,000 lifecycle iterations:

| History records | Badger lifecycle | SQLite lifecycle | Badger database bytes | SQLite database bytes |
|---|---:|---:|---:|---:|
| 0 | 139 µs | 123 µs | 51.6 MB | 41 KB |
| 10,000 | 133 µs | 122 µs | 52.0 MB | 6.6 MB |

Badger has substantial initial file allocation; SQLite is substantially smaller for small frontiers. Lifecycle throughput is comparable in this run, with SQLite slightly faster. Retaining Badger is justified by compatibility with existing storage and its larger-workload results below, not a universal performance advantage. SQLite remains a credible future implementation of `FrontierStore`.

## Dataset larger than the process working memory

The opt-in scale test writes 262,144 distinct, high-entropy 4 KiB records (1 GiB logical history), closes the database, frees Go memory, and reopens it. It uses a 96 MiB Go heap target, the same Badger options as the shipping store with a 64 MiB configured cache budget, and a 16 MiB SQLite page cache. A Go heap target is soft; caches do not impose a hard RSS limit.

| Measurement | Badger | SQLite |
|---|---:|---:|
| Database size | 1.140 GB | 1.226 GB |
| Resident memory after reopen | 34.1 MB | 31.3 MB |
| Resident memory after workload | 124.2 MB | 52.0 MB |
| Go heap after workload | 21.5 MB | 3.4 MB |
| Batched record insertion | 84,569 records/s | 50,251 records/s |
| Pending-key lookup p95, 32 keys | 14.8 µs | 26.4 µs |
| Reopen | 1.52 ms | 0.22 ms |
| Delete 4,096 historical records in batches | 4.29 ms | 28.33 ms |

The workload additionally reads 1,024 scattered history records. Both databases exceed the process's resident set, and the test asserts that relationship. The host's filesystem page cache remains uncontrolled. The observed envelope is therefore **262,144 detailed history records and approximately 1.1–1.2 GB of storage on tmpfs**, not a maximum supported size or a hard production RAM guarantee. Filesystem space may remain allocated after logical deletion; Badger cleanup also attempts value-log GC.

## Traversal graphs under equal budgets

These tests use real frontier persistence, admission, selection, and adaptive feedback. HTTP completion time is simulated with four global slots and one slot per origin. The baseline models the existing publisher's host-at-a-time admission behavior; it is not a replay of the entire legacy runtime. Every strategy has the same 128-request ceiling.

| Graph | Result |
|---|---|
| Deep chain | All strategies: 128 requests, 128 distinct pages, one origin, 6.4 simulated seconds |
| Wide single site | All strategies: 128 requests, 128 distinct pages, one origin, 6.4 simulated seconds |
| Duplicate-heavy | All strategies exhaust the graph after 33 requests and eight distinct HTML bodies; duplicates do not create more fetches |

For mixed-latency origins, one origin takes 500 ms/request and the others take 50 ms/request:

| Strategy | Requests | Origins reached | Distinct pages | Simulated completion time |
|---|---:|---:|---:|---:|
| Host-at-a-time baseline | 128 | 1 | 128 | 64.0 s |
| Breadth-first | 128 | 65 | 128 | 3.0 s |
| Depth-first | 128 | 17 | 128 | 2.1 s |
| Host-diverse | 128 | 65 | 128 | 2.2 s |
| Adaptive | 128 | 16 | 128 | 2.45 s |

Adaptive selection trades breadth for observed yield; it does not universally dominate host-diverse traversal. Host-diverse remains the balanced default. These simulated completion times establish scheduler behavior, not measured internet crawl acceleration.

A real crawl-queue dispatch benchmark with 100 durable enqueue/acknowledgment cycles measured 20.02 ms/request with polling and 90.3 µs/request with notifications. The opt-in notification pump removes the fixed 50-dispatches/second ceiling without changing legacy defaults. Queue depth reads use a rebuildable active-work index and state counts use transactional counters, so neither scans completed history after startup. Legacy databases rebuild those indexes during preparation.

## Correctness and lifecycle checks

The full Go suite and race checks cover frontier workers, queues, and application lifecycle. Dedicated tests exercise:

- Deleted cursor records, bounded cleanup, finite and permanent retention, and expiry allowing rediscovery.
- Stable publication reservations across restart and partial publication failures.
- Disk-headroom failures, cancellation, budgets, and capacity limits.
- Durable staged discovery, replay, restart, and automatic admission after capacity opens.
- Supervised site traversal with `max_pending: 1`, followed by pause, staged work, stop/restart recovery, and resume.
- Strategy ordering, exploration, origin rotation, and origin cooldowns.

## Reproduction

```sh
go test ./...
go test -race ./frontier ./worker/frontierworker ./queue ./application
go test ./frontier -run '^$' -bench BenchmarkStorageWorkload -benchtime=1000x
go test ./frontier -run '^$' -bench BenchmarkTraversalGraphs -benchtime=1x
go test ./queue -run '^$' -bench BenchmarkCrawlDispatchModes -benchtime=100x
DELVER_FRONTIER_SCALE=1 go test ./frontier -run '^TestFrontierScale$' -count=1 -v -timeout=10m
```

The scale test requires Go 1.19 or newer and roughly 2 GB of free temporary storage per active backend. Rerun it on the intended persistent volume before sizing a deployment. Retention controls terminal history; protected staged discoveries and active work still require disk capacity, and permanent deduplication grows with unique URLs.

See [final-code endurance and scale validation](endurance-validation.md) for the subsequent million-URL tests of the shipping frontier APIs, including terminal history, restart recovery, expiry, and deleted-cursor seeking. The 262,144-record storage workload above measures a separate high-entropy payload envelope.
