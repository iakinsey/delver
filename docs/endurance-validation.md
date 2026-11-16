# Historical Final-code endurance and scale validation

These measurements describe an earlier revision. Current acceptance requires rerunning the commands in `Makefile` against final code. Generated evidence is stored privately outside Git.
These opt-in tests exercise the shipping Badger frontier and worker application. They use isolated storage and synthetic destinations served by local fixtures. No production crawl or Elasticsearch data is used.

## Workloads

`TestWorkspaceSoak` uses the real clock for one hour, with the Go race detector enabled. Two independent crawls run simultaneously in fixed and adaptive mode. They crawl cyclic pages on sixteen synthetic origins through a local HTTP proxy, with injected HTTP 429/503/404 responses, stalled requests, and truncated bodies. The test verifies the user agent, disabled robots requests, process and adaptive bounds, continued progress, empty failed queues, bounded metric cardinality, sampled live heap, and goroutine counts. Short retention and revisits exercise repeated admission and cleanup. At fifteen, thirty, and forty-five minutes it closes and reopens the workspace and resumes the same run IDs. Every restart and final shutdown compares the persisted metric totals exactly with the closed controllers' totals.

`TestFrontierMillionURLs` admits 1,000,000 distinct normalized URLs through `Store.Admit`, using 10,000 origins and a 64 MiB configured cache budget. The pending ceiling is explicitly raised to cover the workload. It probes all four scheduling indexes, checks exact deduplication, rebuilds the fresh-work indexes from the full active dataset, and transitions real requests through reservation, publication, and outcome APIs. It also exercises protected delayed retries, new retry identities, a deleted seek cursor, a separate process exiting without closing Badger, synchronous-write recovery, bounded expiry, rediscovery after expiration, and permanent retention. Logical URL counts and transactional counters are checked after cleanup.

The tmpfs case starts empty and builds approximately 900,000 terminal records before expiry. The persistent-filesystem case uses a smaller 10,000-record terminal cohort after reaching one million admissions. Its admission was interrupted after 250,665 URLs to apply the write-memory correction; the final test reopened that dataset, checked its saved sequence, and continued with the corrected implementation. Both cases perform the same cursor, retry, restart, expiry, and permanent-retention checks. The results must therefore distinguish full-history tmpfs measurements from persistent-disk measurements.

The tests collect JSONL samples. Scale guards remain 512 MiB sampled live Go heap and 2 GiB RSS; the soak guards are 768 MiB sampled live heap and 1,500 goroutines, followed by a shutdown goroutine check. Live heap is sampled after explicit GC; RSS includes memory mapped by Badger. These are workload assertions, not a universal process-memory guarantee.

## Final-code results (November 15–16, 2026)

The full Chromium suite passed **1,306 tests** in 29.9 minutes. All **83 frontend unit tests**, the uncached full Go suite (`go test -count=1 ./...`), the uncached full Go race suite (`go test -race -count=1 ./...`), `go vet ./...`, the Go production build, and the frontend/backend option-catalogue check passed. A fresh production UI build matched every embedded asset byte for byte. The entire gateway suite also passed under the race detector with a separate live Elasticsearch 9.5.6-SNAPSHOT node, including search/streaming and crawl-log deletion isolation. The existing 1 GiB high-entropy storage test passed for Badger and SQLite (262,144 records per engine; 23.37 seconds total).

The tmpfs million-URL lifecycle passed in **2,235.97 seconds**. It admitted 1,000,000 initial URLs, transitioned 900,000 through the normal outcome path, recovered 100 synchronous admissions from a child process exiting without closing the database, protected and completed 64 delayed retries, expired 900,128 terminal URL records, and preserved seeking after the cursor record was deleted. Final counters were 1,000,102 admissions, 900,129 terminal outcomes, 99,973 pending/active URLs, zero reserved requests, zero pending retries, and 1,800,256 cleanup operations. The final terminal outcome is the explicit never-expire record; rediscovery accounts for the other additional admission.

The largest sampled live heap was **369.8 MiB** and the largest sampled RSS was **1,167.9 MiB**, both during the full active-index rebuild. Rebuilding those indexes took 69.12 seconds. Across all four strategies, the largest p95 for selecting 128 candidates was **56.8 ms**, measured with 900,000 historical outcomes present. These timings overlapped other test workloads and are validation observations, not isolated hardware benchmarks.

The one-hour whole-application soak passed under the Go race detector in **3,602.12 seconds** (including setup and shutdown), with **26,858 proxy requests**, all three workspace restarts, and exact final metric checkpoint comparisons. It injected 276 stalled requests, 1,514 overload responses, and 354 truncated bodies. There were no race reports or failed queue/health assertions. Maximum sampled live heap was **423.3 MiB**, peak goroutines were **238**, and the final goroutine shutdown check passed. All destinations were local synthetic fixtures; no Tor crawl was tested.

The persistent-disk million-URL case passed, including the same retry, deleted-cursor, unclean-process, expiry, rediscovery, and permanent-retention assertions. Its resumed process completed in **7,127.63 seconds**; this excludes the earlier admission of 250,665 URLs and must not be read as the duration of an empty-to-million run. The remaining 749,335 admissions used synchronous writes while other tests ran. The full active-index rebuild took **32.80 seconds**. Peak sampled live heap was **274.2 MiB**, peak sampled RSS was **1,216.0 MiB**, and the largest p95 for selecting 128 candidates was **9.8 ms**. Expiration removed 10,128 terminal URL records in six bounded cleanup calls (1.29 seconds). Final counters were 1,000,102 admissions, 10,129 terminal outcomes, 989,973 pending/active URLs, zero reserved requests, zero pending retries, and 20,256 cleanup operations.

Earlier raw evidence was removed from Git with generated artifacts; retain new reports in the external validation directory.

## Defects reproduced and corrected

- A queue dispatch conflict could latch an unhealthy state and cause admission backpressure to prevent recovery. Queue mutations now serialize their shared state counters, and transient transaction contention does not latch health. A regression runs eight producers and sixteen consumers across 1,000 durable deliveries, including staged outputs and acknowledgments.
- Large configured retry and cleanup batches could exceed Badger's transaction size. They now split into smaller atomic transactions while preserving the configured row ceiling. A regression uses 2,048 large URLs to force splitting and verifies identities, counters, expiry, and protected pending work.
- Cancellation could checkpoint metrics before admitted workers finished recording their attempts. Orderly close now writes a final checkpoint after worker shutdown. The regression first failed with one saved attempt instead of two; it passes with both saved.
- The cleanup job's fixed one-minute timer ignored shorter retention intervals. The compiled timer now uses the configured interval. A live-crawl regression verifies actual detail and dedup expiration with a 50 ms cleanup cadence.
- Rapid admission retained large Badger table-builder/compression allocator buffers beyond the configured read caches. The failing scale profile attributed about 97% of live heap to allocation dominated by those buffers. Table targets now derive from the cache budget, stop growing across levels, and use two compaction workers. The same 512 MiB heap guard is retained.
- Pressure sampling copied the entire application configuration while startup was assigning the HTTP client controller and phase timeouts. The race-enabled soak exposed this during workspace restart. Storage paths are now captured before controller startup and reused by the sampler. A regression reproduced the race before the fix and passed twenty race-enabled repetitions afterward.
- The paused-duration browser test allowed only fifteen seconds for a fifteen-second active-time budget plus drain completion. Its completion wait now covers that budget and drain time, and it additionally verifies that saved elapsed time remains exactly unchanged throughout the pause.

## Reproduction

From the repository root:

```sh
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...

DELVER_SOAK=1 DELVER_SOAK_DURATION=1h \
  DELVER_VALIDATION_ROOT=/path/to/isolated/soak \
  go test -race ./application -run '^TestWorkspaceSoak$' -count=1 -timeout=75m -v

# Full million-URL lifecycle, including a large completed-history cohort.
DELVER_FRONTIER_MILLION=1 \
  DELVER_VALIDATION_ROOT=/tmp/delver-million-validation \
  go test ./frontier -run '^TestFrontierMillionURLs$' -count=1 -timeout=45m -v

# Persistent disk: keep the terminal cohort smaller to separate admission/fsync cost.
DELVER_FRONTIER_MILLION=1 DELVER_FRONTIER_HISTORY_URLS=10000 \
  DELVER_VALIDATION_ROOT=/path/on/persistent/disk \
  go test ./frontier -run '^TestFrontierMillionURLs$' -count=1 -timeout=150m -v

cd web
npm test
npm run test:browser -- --workers=4
```

The storage tests retain failing fixture directories for diagnosis and remove successful fixture data. Reports remain in the specified validation directory. Tmpfs does not measure physical-disk flush latency or power-loss durability; the separate process recovery checks test process exit and WAL recovery while the operating system remains running. The OS page cache is uncontrolled.
