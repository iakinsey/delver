# Operations and recovery

## Storage layout

Use separate directories for each queue, dead-letter queue, crawl database, object store, and persistent map. File queues exclusively lock their directory while running; Badger also enforces exclusive database ownership. Never manually copy a live Badger directory or run another process against it. Stop the application before backing up its storage.

File queue records are JSON envelopes. Temporary body files begin with `.object-`; queue writes use `.writing`; delivered records use `.claimed`. A `.claimed.outputs` sidecar retains a serialized output batch. Do not remove that sidecar independently of its input. The `.lock` file is ownership metadata, not a message.

Crawl databases retain pending, leased, completed, and failed URL records under the configured crawl ID. Completed URLs remain deduplicated across restarts. Changing `crawl_id` starts an independent namespace. `recrawl_after` permits a completed URL to be scheduled again when a publisher or discovery worker submits it; it does not independently generate timer events.

## Stop and resume

Ctrl-C and SIGTERM cancel the application. In-flight publication may be interrupted; persistent queues retain the input for replay. Bodies referenced by unacknowledged extraction work remain available. Job timers stop, managers wait for their loops, and storage closes. Increase `shutdown_timeout` when custom workers perform long operations; implement context-aware processing for prompt cancellation.

Restart with the same configuration and storage. Set file queue `reset: true` so abandoned claimed records become ready again after exclusive ownership is acquired. Crawl queues recover leased records automatically. Output batches are reused, so a retry does not need to regenerate deleted inputs or invent new output identities. Startup reconciliation removes orphaned bodies after inspecting pending queue references.

## Failed work

File queues retry a failed transaction up to `max_attempts`, then move it to `dlq_path`. Corrupt messages are quarantined there. Stop the application before inspecting or replaying records. Correct the underlying failure and JSON payload before moving a dead-letter record back under its original queue filename. Reset its `attempts` field if another full retry budget is intended. Move any corresponding `.claimed.outputs` sidecar with the record so partially published outputs retain their identities. Never move records into an active queue directory by overwriting existing files. Dead-letter references retain their fetched bodies for replay.

Crawl requests exhausted after `max_attempts` remain in the database with state `failed` and are not repeatedly dispatched. A new crawl namespace retries those URLs. A network/robots failure normally travels as a fetch response with `success: false` and an error string, rather than retrying the entire pipeline indefinitely. Inspect both response metadata and queue failures.

At-least-once output can duplicate JSONL lines or custom external side effects after a crash. Deduplicate by envelope ID or request ID when consuming such outputs. Elasticsearch composites use the request ID as document ID.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| No fetches on restart | URLs may already be completed; choose a new crawl ID or configure recrawling |
| Requests denied | `allowed_domains`, robots rules, error metadata, redirects, and supported URL schemes |
| Queue capacity exceeded | Increase `max_size`, restore the downstream consumer, or reduce publisher volume |
| Cannot open storage | Another process may own the directory; check path permissions and parent directories |
| Missing dependent feature | Enabled dependency list and `extractor_errors` in the analysis |
| Sentiment absent | It requires title and language; only English titles are classified |
| Results missing | Selected run, configured sink, queue health, and Elasticsearch indexing errors |
| Crawl stops discovering early with Bloom mode | Capacity/probability and false positives; prefer exact crawl queues |

## Metrics and indexing

A configured queue named `transformer_queue` enables the metrics sink when `metrics.enabled` is true. A transformer consuming it can index `metric` and `composite` messages. Counters report interval totals; samples report means. Message output counts correspond to actual batch sizes.

Elasticsearch bulk requests complete before indexing is acknowledged. Transport, HTTP, and per-item failures return an error to the worker manager and preserve the input for retry. The supplied mappings use wildcard fields and case-insensitive literal queries. Live integration was verified on Elasticsearch 9.5.6-SNAPSHOT. Ordinary tests use local fixtures; run the opt-in live regression and manual workflow in [manual testing](manual-testing.md) against your cluster.
