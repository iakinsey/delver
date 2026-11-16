# Reproducible validation

Install Go (see `go.mod`), a C compiler for SQLite, Node 22.22 or newer, and supported Playwright browser dependencies. `make test-setup` installs the declared frontend dependencies and managed Chromium. No private test backup is required.

| Command | Workload |
| --- | --- |
| `make build` | Production UI, embedded assets, executable |
| `make test` | All backend and frontend unit/integration suites |
| `make test-race` | Backend race detector |
| `make test-static` | Vet, Go/JS formatting, whitespace |
| `make test-ui` | Targeted browser inventory against real backend, at most 30 minutes |
| `make test-benchmark BASELINE=/private/baseline.log BASELINE_APP=/private/pipeline-baseline.log` | Five isolated fixed-capacity repetitions and comparison |
| `make test-soak` | One-hour race-enabled application soak, three restarts |
| `make test-million TMPFS_ROOT=/dev/shm/unique-empty-directory` | Million URLs from empty tmpfs storage |
| `make test-million-disk DISK_ROOT=/persistent/unique-empty-directory` | Million URLs from empty persistent storage |
| `make test-elasticsearch ES_URL=http://127.0.0.1:19200` | Isolated live indexing/query/deletion |
| `make test-tor TOR_CONFIG=/private/seeds.json` | Explicit 30-minute Tor validation |
| `make validate-local` | Sequential local gates |
| `make validate` | Local gates, then Tor |

Set `VALIDATION_ROOT` outside the repository. Missing prerequisites fail. Normal unit runs do not opt into live/endurance tests; invoke their explicit targets for acceptance. Tests are individually selectable with `go test -run`, `npm --prefix web test -- <file>`, or `npm --prefix web run test:browser -- <spec> --grep <name>`.

Capture both benchmark baselines before modifying performance code (or run the same self-contained pipeline benchmark in the saved checkpoint checkout):

```sh
go test ./util -run '^$' -bench BenchmarkFetchCapacity -benchtime=1x -count=5 > /private/baseline.log
go test ./application -run '^$' -bench BenchmarkWorkspaceCapacity -benchtime=1x -count=5 > /private/pipeline-baseline.log
```

Run baseline and final benchmarks without competing heavy workloads. Preserve raw output. Median fixed successful throughput at each slot count must remain within 10% of baseline. Adaptive recovery is asserted separately in `flow/recovery_test.go`.

The soak retains simultaneous fixed/adaptive crawls, injected failures and admission pressure, checkpoint comparisons, three workspace restarts, and shutdown checks. Scale workloads exercise traversal indexes, retries, rebuilding, crash recovery, expired seek cursors, bounded expiration, rediscovery, and permanent retention. Heap/RSS/goroutine guards remain in the test code.

Elasticsearch must be an isolated test cluster. Live tests create unique indices and remove those indices on cleanup. Do not point the target at operational crawl data.

Tor uses the existing externally managed deployment unchanged. `TOR_CONFIG` contains the prior private seeds/settings; the test requires a proxy and onion destinations, writes five-minute reports, pauses/resumes, reopens the isolated workspace, and verifies shutdown. `TOR_PROXY` defaults to `socks5h://127.0.0.1:5566`. External network failures are inconclusive and must be documented rather than treated as a pass. Controlled HTTP/SOCKS fixtures assert headers, robots, no direct fallback, and pressure recovery.
