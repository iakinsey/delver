# Crawler operations

The default executable now opens the multi-crawl [workspace](workspace.md), including guided configuration and per-crawl proxies. The controls below also describe the retained legacy supervisor API.

The CLI supervises one crawler runtime. Its API and result streamer remain available when the crawler stops. Existing Go integrations can still call `application.Run`; use `application.Serve` for supervised services.

Enable the API and set `config.api.operations_path` to a private persistent file; the default is `dash_db_path + ".operations.json"`. Open the frontend and select **Crawler** from the gear menu. All controls are directly available. The state file contains crawl configuration and external-service secrets and uses mode 0600.

## Lifecycle

Start, pause, resume, and stop commands return an operation record. Acceptance is separate from completion. Pause suspends scheduled publishers, drains consumed queues and active work, then suspends workers. A drain timeout reports an error and leaves the runtime visibly pausing; resume restores processing. Stop preserves persistent queues and closes resources. Channel queues remain transient.

The desired running/paused/stopped state and active configuration survive restart. A saved configuration takes precedence over the original CLI configuration after the first supervised run. API bindings, dashboard storage, and streaming bindings remain process settings. Interrupted command records become failed on startup; inspect state before issuing a new command.

## Configuration

The Configuration view supports fixed URL and RSS source forms, plus the complete pipeline JSON. Source forms append a publisher targeting the configured fetcher's inbox, with a five-minute interval editable in JSON. Validate, save a draft, then explicitly activate it. Activation stops the old runtime before creating its replacement; rollback selects the previous version. A startup failure attempts restoration of the previous active configuration. A runtime that cannot shut down safely prevents replacement.

Configuration reads redact recognized credential fields, URL user information, and credential query parameters, including custom resource and worker parameters. Unchanged `__REDACTED__` placeholders preserve values from the saved draft or active configuration. Source summaries and diagnostics also redact configured credentials. API service and streaming service settings require a process restart; UI submissions cannot change them.

## API

All crawler routes require an client and POST JSON. Responses use the existing API envelope.

| Route | Request |
| --- | --- |
| `/crawler/status` | `{}`; lifecycle state, version, worker activity, queue depths and health |
| `/crawler/operations` | `{}`; durable command records with outcomes |
| `/crawler/draft` | `{}` to read; `{"configuration": {...}}` to validate and save |
| `/crawler/validate` | `{"configuration": {...}}`; validation only |
| `/crawler/command` | `{"id":"unique-command-id","action":"start"}` |
| `/crawler/failed` | `{"queue":"requests","offset":0}`; up to 50 failed records |

Commands support `start`, `pause`, `resume`, `stop`, `activate`, `rollback`, `replay`, and `recrawl`. Recovery commands include `queue` and `record`. Replay uses the failed record ID; recrawl uses a normalized completed URL from a crawl queue. Command IDs are idempotent and cannot be reused for a different action or record. Only one command executes at a time.

Recovery runs through the active runtime's queue objects, never through a second database owner. File replay writes a durable intent before restoring outputs and the message; interrupted replay is completed before the queue pump starts. Corrupt records remain inspectable but must be repaired before replay. Start the runtime before inspecting or recovering records. Replay preserves output IDs, staged batches, and body references while resetting retry eligibility. Recrawl creates a new message identity and discards the completed request's staged output.

## Execution limits

Composite extractors accept `parameters.timeout` (default `30s` per dependency stage). Context-aware extractors implement the optional `extractors.ContextExtractor` interface. Built-ins check cancellation while reading, parsing, matching, and iterating. Language detection samples at most 64 KiB; sentiment titles are limited to 64 KiB. Legacy extension code cannot be forcibly terminated: shutdown waits for outstanding extractor calls, and a timeout prevents a replacement runtime from opening its resources.

RSS fetch concurrency defaults to eight and can be set with `parameters.max_concurrency`. Feeds respect the configured HTTP response-size limit. Feed errors are exposed in worker health, successful feed results are retained, and periodic execution retries feeds using the HTTP client's configured retry policy.

A complete local setup is available in `examples/operations.json`. Start `go run ./examples/site`, then `go run . examples/operations.json`, and run the frontend as described in `web/README.md`. Open the UI directly. API-disabled CLI runs retain the original unsupervised behavior and do not create operations state.

The opt-in [frontier workers](frontier.md) add configurable traversal strategies, durable admission backpressure, and retention. [Benchmark results and reproduction commands](frontier-benchmarks.md) describe the measured operating envelope.
