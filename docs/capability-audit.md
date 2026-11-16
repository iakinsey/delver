# Capability audit and acceptance evidence

The repair scope is the existing single-instance HTTP framework, its extension interfaces, and a complete operator UI. Browser rendering, distributed scheduling, hot reload, and new news-analysis models are outside that scope.

## Existing framework

| Capability | Result | Evidence |
| --- | --- | --- |
| Fixed seeds, RSS/Atom, DFS and news pipelines | Verified end to end; direct publication and discovery storage failures now propagate rather than acknowledging lost work | `TestDocumentedExamples`, `TestLocalCrawl`, `TestDFSStorageFailurePreventsAcknowledgment`, `TestNewsPublicationCancellationAndStableIdentity` |
| HTTP retries, size limits, proxies, robots, redirects and scope | Existing policies retained; library configuration errors no longer terminate the host process | Fetcher, frontier and HTTP regression suites; application local crawl test |
| RSS concurrency, partial failure and cancellation | Configurable bounded concurrency; successful feeds survive other feed failures; failures remain visible and are retried by HTTP policy and later scheduled executions; feed bodies are bounded | `TestRSSCancellation`, `TestRSSPartialFailureHealthAndConcurrency`, `TestRSSBodyLimit` |
| Extractor dependency execution | Deadlines and cooperative cancellation; panics become errors; legacy calls remain tracked until cleanup | `TestBuiltInExtractorsCancelDuringWork`, `TestExtractorTimeoutRetainsLegacyExecution`, `TestExtractorPanicBecomesAnError` |
| URL, title, text, language, mentions, ngrams, sentiment and adversarial extraction | Existing output contracts retained; CPU loops check cancellation; language sampling and sentiment-title work are bounded; enumeration detection is linear | Extractor tests, including Unicode, URL/base/redirect, subdomain and sentiment regressions |
| Exact crawl state | Transactional pending index; bounded conflict retries; fresh recrawl identities and rejection of stale acknowledgments | Crawl tests, `TestConcurrentCrawlRecoveryAndStaleAcknowledgment` |
| File queues, transient channels and timers | Durable staged batches and bounded queues retained; timer queues explicitly remain producers; non-resilient queue errors reach the runtime | Queue regressions, `TestPersistentOutputReplay`, `TestFullOutboxRetainsStagedInputForRecovery`, timer wiring validation |
| Body lifetime and reconciliation | Failed work and replay intents retain body references; committed work remains eligible for cleanup | Queue object-reference tests and interrupted replay tests |
| Badger maps and Bloom filters | Existing persistence tests pass; DFS domain consumption now shares locking with writers and deletes a domain only after successful publication | Map/Bloom suites, `TestConsumeHostRetainsFailedBatchAndSerializesWriters`, publisher partial-failure regression |
| Configuration and custom components | Strict JSON, precise integers, bounded configuration input and preflight validation; custom parameter syntax remains owned by its factory | `TestStrictConfiguration`, `TestConfigurationPreservesIntegerPrecision`, `TestDecodeRejectsTrailingAndOversizedInput`, `TestCustomParametersKeepTheirOwnDurationSemantics` |
| JSONL and Elasticsearch | Existing delivery semantics retained; indexing and declaration requests are bounded and cancellable | Sink/search tests, `TestIndexCancellation`, `TestLiveElasticsearch` |

## Runtime and recovery

| Requirement | Evidence |
| --- | --- |
| Separate API lifetime; crawler startup failure remains manageable | `TestServiceRemainsAvailableAfterCrawlerStartupFailure` |
| Start, drain/pause, resume, stop and restart | `TestSupervisorLifecycleAndPersistence`, `TestSupervisedCrawlPauseRecrawlAndRestart` |
| Persisted pause with outstanding work | The supervised crawl test restarts while a recrawl is queued, verifies no additional output before resume, then completes it |
| Responsive monitoring during transitions | `TestStatusRemainsAvailableDuringStartupAndShutdown`; snapshots include source summaries, crawl IDs, worker activity, queue states and errors |
| Exclusive operations-state and queue ownership | `TestOperationsStateHasExclusiveOwner`, `TestShutdownTimeoutRetainsOwnershipUntilCleanup`, queue lock regressions |
| Validated activation, automatic restoration and rollback | `TestSupervisorDraftValidation`, `TestFailedActivationRestoresRunningConfiguration`, `TestRollbackFailureRemainsObservableAndDoesNotClaimCompletion` |
| Persistence errors and shutdown errors are visible | `TestDraftSaveFailureDoesNotChangeConfiguration`, `TestResourceCloseFailureIsReported`, API database-failure test |
| Shutdown during commands and interrupted command recovery | `TestCloseDuringCommandWaitsForConstructionCleanup`, `TestInterruptedOperationIsNotRepeated`, `TestSupervisorShutdownRejectsCommands` |
| Durable command identities and conflict handling | `TestConcurrentCommandIDsAreIdempotent`; operation records include actor, action, queue, record and outcome |
| Replay preserves staged output identity and retry eligibility | File/crawl recovery tests and full-outbox recovery test |
| Replay interruption, storage failure and concurrency | `TestFileReplayRestoresEveryInterruptedPhase`, `TestRecoveryStorageFailurePreservesIntent`, `TestConcurrentFileRecoveryDoesNotDuplicateRecord` |
| Corrupt data remains inspectable and cannot be activated blindly | `TestCorruptDeadLetterCanBeInspectedButNotReplayed`, `TestCorruptedOperationsStateFailsWithoutStarting` |
| Credentials remain server-side | `TestConfigurationSecretsAreRedactedAndRoundTrip`; redaction also covers URL credentials, query tokens and diagnostics |

## UI and validation

The real API browser workflow adds fixed and RSS sources, validates and activates the configuration, observes both publishers executing, pauses the pipeline, replays failed work, schedules recrawl, stops, and rolls back. It checks direct access, accessibility, and 375/768/1440px layouts. The separate failure workflow covers invalid JSON, failed saves, unsaved navigation, pagination and idempotent retries after a lost command response.

Run the complete validation with:

```sh
DELVER_ES_URL=http://localhost:9200 go test -race -timeout 120s ./...
go vet ./...
cd web
npm test
npm run build
DELVER_ES_URL=http://localhost:9200 npm run test:browser
```

Use Node 22.22 or newer. Browser Elasticsearch fixtures create and remove uniquely named indices. The dispatch benchmark (`go test -run '^$' -bench BenchmarkCrawlDispatch -benchtime=3x ./queue`) checks 0, 1,000 and 10,000 completed records; results include the existing 20 ms poll interval and are not a production throughput guarantee.

Delivery remains at least once, JSONL may contain duplicates, and transient channels do not survive restart. Go cannot forcibly terminate arbitrary custom extension code; unfinished legacy calls retain resource ownership and prevent replacement runtimes. Native extractors cooperate with cancellation. Language detection samples at most 64 KiB, and sentiment rejects titles larger than 64 KiB.

Final validation on 2026-11-14 passed the complete Go race suite with live Elasticsearch enabled, `go vet`, all 16 frontend unit tests, the production build, and all 9 browser tests with live Elasticsearch and Node 22.23.5. The application/queue/map stress run also passed three consecutive executions under the race detector. The frontend build retains its pre-existing bundle-size advisory. Responsive screenshots for the crawler workflow are generated in `web/test-results/crawler-{375,768,1440}.png`.

The account system has been removed from production code. Direct dashboard/crawler access is covered by `TestUnrestrictedDashboardAndCrawler`; schema preservation and rollback by `TestDashboardMigrationPreservesAllLayoutsAndRemovesOwnership` and `TestDashboardMigrationFailureRollsBackAndRetries`; configuration/state upgrades by `TestObsoleteAccountSettingsAreRemovedDuringUpgrade` and `TestPersistedStateUpgradeRemovesAccountFields`. All browser workflows now begin without stored credentials.
