.DEFAULT_GOAL := build
.NOTPARALLEL: validate-local validate
VALIDATION_ROOT ?= $(HOME)/.cache/delver-validation/current
BASELINE_APP ?= $(VALIDATION_ROOT)/benchmark-pipeline-baseline.log
BASELINE ?=
DISCOVERY_BASELINE ?=
DISCOVERY_PIPELINE_BASELINE ?=
ES_URL ?=
TOR_CONFIG ?=
TOR_PROXY ?= socks5h://127.0.0.1:5566
TMPFS_ROOT ?= /dev/shm/delver-validation
MILLION_DISK_TIMEOUT ?= 4h
DISK_ROOT ?= $(HOME)/.cache/delver-validation/million-disk
.PHONY: build ui test-setup test test-race test-static test-ui test-ui-coverage test-inventory test-benchmark test-discovery-benchmark test-soak test-million test-million-disk test-storage-scale test-elasticsearch test-tor validate-local validate
build: ui
	go build -o build/delver .
ui:
	npm --prefix web run build
	node scripts/package-ui.mjs
test-setup:
	npm --prefix web ci
	cd web && npx playwright install chromium
test-inventory:
	node scripts/check-test-inventory.mjs
test: test-inventory
	go test -count=1 ./...
	npm --prefix web test
test-race:
	go test -race -count=1 ./...
test-static:
	go vet ./...
	@test -z "$$(gofmt -l $$(rg --files -g '*.go'))" || (echo 'Go formatting check failed'; exit 1)
	cd web && npx prettier --check src e2e playwright.config.js vite.config.js
	git diff --check
test-ui:
	DELVER_VALIDATION_ROOT="$(VALIDATION_ROOT)" timeout --signal=TERM --kill-after=5s 1790s node scripts/run-ui-tests.mjs
test-ui-coverage:
	node scripts/check-ui-coverage.mjs "$(VALIDATION_ROOT)/browser-results.json"
test-benchmark:
	DELVER_VALIDATION_ROOT="$(VALIDATION_ROOT)" DELVER_BENCHMARK_BASELINE="$(BASELINE)" DELVER_PIPELINE_BASELINE="$(BASELINE_APP)" node scripts/compare-benchmark.mjs
test-discovery-benchmark:
	DELVER_VALIDATION_ROOT="$(VALIDATION_ROOT)" DELVER_DISCOVERY_BASELINE="$(DISCOVERY_BASELINE)" DELVER_DISCOVERY_PIPELINE_BASELINE="$(DISCOVERY_PIPELINE_BASELINE)" node scripts/compare-discovery.mjs
test-soak:
	DELVER_SOAK=1 DELVER_SOAK_DURATION=1h DELVER_VALIDATION_ROOT="$(VALIDATION_ROOT)/soak" go test -race ./application -run '^TestWorkspaceSoak$$' -count=1 -timeout=75m -v
test-million:
	@test -z "$${DELVER_FRONTIER_RESUME_ROOT}" || (echo 'Acceptance scale workloads must start from empty storage; unset DELVER_FRONTIER_RESUME_ROOT'; exit 1)
	@test -d /dev/shm && test "$$(stat -f -c %T "$(dir $(TMPFS_ROOT))")" = tmpfs || (echo 'TMPFS_ROOT must be on tmpfs'; exit 1)
	DELVER_REPEATED_SCALE=1 DELVER_FRONTIER_MILLION=1 DELVER_FRONTIER_URLS=1000000 DELVER_FRONTIER_HISTORY_URLS=900000 DELVER_VALIDATION_ROOT="$(TMPFS_ROOT)" go test ./frontier -run '^TestFrontierMillionURLs$$' -count=1 -timeout=90m -v
test-million-disk:
	@test -z "$${DELVER_FRONTIER_RESUME_ROOT}" || (echo 'Acceptance scale workloads must start from empty storage; unset DELVER_FRONTIER_RESUME_ROOT'; exit 1)
	@mkdir -p "$(DISK_ROOT)"
	@test "$$(stat -f -c %T "$(DISK_ROOT)")" != tmpfs || (echo 'DISK_ROOT must be on persistent storage'; exit 1)
	DELVER_REPEATED_SCALE=1 DELVER_FRONTIER_MILLION=1 DELVER_FRONTIER_URLS=1000000 DELVER_FRONTIER_HISTORY_URLS=10000 DELVER_VALIDATION_ROOT="$(DISK_ROOT)" go test ./frontier -run '^TestFrontierMillionURLs$$' -count=1 -timeout=$(MILLION_DISK_TIMEOUT) -v
test-storage-scale:
	DELVER_FRONTIER_SCALE=1 go test ./frontier -run '^TestFrontierScale$$' -count=1 -timeout=20m -v
test-elasticsearch:
	@test -n "$(ES_URL)" || (echo 'ES_URL must identify an isolated test Elasticsearch instance'; exit 1)
	DELVER_ES_URL="$(ES_URL)" go test -race ./gateway -run 'Test(LiveElasticsearch|LiveProtection|LiveCrawlLogDeletionIsolation|Delete)' -count=1 -v
	DELVER_ES_URL="$(ES_URL)" go test -race ./application -run '^TestLive(WorkspaceLogDeletionOptions|ReportIndexedHistory|ProtectionElasticsearchOutageAndRetry)$$' -count=1 -v
test-tor:
	@test -f "$(TOR_CONFIG)" || (echo 'TOR_CONFIG must identify a private crawl definition containing test seeds'; exit 1)
	DELVER_TOR_LIVE=1 DELVER_TOR_CONFIG="$(TOR_CONFIG)" DELVER_TOR_PROXY="$(TOR_PROXY)" DELVER_VALIDATION_ROOT="$(VALIDATION_ROOT)/tor" go test ./application -run '^TestLiveTor$$' -count=1 -timeout=40m -v
validate-local: build test test-race test-static test-ui test-benchmark test-soak test-million test-million-disk test-storage-scale test-elasticsearch
validate: validate-local test-tor

.PHONY: test-report-million test-report-million-disk test-report-graph
test-report-million:
	@test -d /dev/shm && test "$$(stat -f -c %T /dev/shm)" = tmpfs || (echo 'Report scale test requires tmpfs at /dev/shm'; exit 1)
	DELVER_REPORT_MILLION=1 DELVER_REPORT_SCALE_ROOT=/dev/shm DELVER_VALIDATION_ROOT="$(VALIDATION_ROOT)/report-million-tmpfs" go test ./report -run '^TestReportMillionURLs$$' -count=1 -timeout=20m -v
test-report-million-disk:
	@mkdir -p "$(DISK_ROOT)"
	@test "$$(stat -f -c %T "$(DISK_ROOT)")" != tmpfs || (echo 'DISK_ROOT must be persistent storage'; exit 1)
	DELVER_REPORT_MILLION=1 DELVER_REPORT_SCALE_ROOT="$(DISK_ROOT)" DELVER_VALIDATION_ROOT="$(VALIDATION_ROOT)/report-million-disk" go test ./report -run '^TestReportMillionURLs$$' -count=1 -timeout=20m -v
test-report-graph:
	DELVER_REPORT_GRAPH_SCALE=1 DELVER_REPORT_SCALE_ROOT="$(DISK_ROOT)" DELVER_VALIDATION_ROOT="$(VALIDATION_ROOT)/report-graph" go test ./report -run '^TestReportHundredThousandHostGraph$$' -count=1 -timeout=20m -v

.PHONY: test-network-million
test-network-million:
	DELVER_ATLAS_MILLION=1 go test ./report -run '^TestAtlas(MillionHosts|RepeatedFoldingScale)$$' -count=1 -timeout=20m -v

.PHONY: test-protection-benchmark
test-protection-benchmark:
	@test -n "$(BASELINE_BIN)" || (echo 'BASELINE_BIN must identify the pre-change binary'; exit 1)
	python3 scripts/benchmark-protection.py --baseline "$(BASELINE_BIN)" --final "$(CURDIR)/build/delver" --output "$(VALIDATION_ROOT)/protection-benchmark" --storage-root "$(TMPFS_ROOT)/protection-benchmark"

.PHONY: test-efficiency-benchmark
test-efficiency-benchmark:
	@test -n "$(BASELINE_TEST_BIN)" && test -n "$(CANDIDATE_TEST_BIN)" && test -n "$(EFFICIENCY_BENCHMARK)" || (echo 'BASELINE_TEST_BIN, CANDIDATE_TEST_BIN and EFFICIENCY_BENCHMARK are required'; exit 1)
	python3 scripts/benchmark-efficiency.py --baseline "$(BASELINE_TEST_BIN)" --candidate "$(CANDIDATE_TEST_BIN)" --benchmark "$(EFFICIENCY_BENCHMARK)" --benchtime "$(or $(BENCHTIME),3s)" --cpu "$(or $(BENCH_CPU),0)" --procs "$(or $(BENCH_PROCS),1)" --output "$(VALIDATION_ROOT)/efficiency-benchmark"
