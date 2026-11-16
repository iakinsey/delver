# Workspace API

The default application serves the embedded UI and JSON API at http://127.0.0.1:8080. Run `go run .` without a scenario file. Bindings and access control belong to the deployment boundary.

All workspace actions use POST JSON. Responses include `http_code`, `success`, optional `data`, and an `error` on failure. Mutations supply the current revision and an idempotency ID where supported. The frontend client implements the request shapes in `web/src/views/page/crawlApi.js`.

| Route | Purpose |
| --- | --- |
| `/crawls/defaults`, `/crawls/preview` | Guided defaults and validation preview |
| `/crawls/list`, `/crawls/detail` | Saved crawls and run snapshots |
| `/crawls/create`, `/crawls/draft`, `/crawls/pipeline` | Create and prepare guided/custom configuration |
| `/crawls/activate`, `/crawls/start_run`, `/crawls/continue_run` | Activate draft, replace run, continue pending work |
| `/crawls/command` | Serialized lifecycle and queue-recovery commands |
| `/crawls/status`, `/crawls/operations` | Runtime state and operation journal |
| `/crawls/results` | Bounded current/historical result previews |
| `/crawls/duplicate`, `/crawls/archive`, `/crawls/delete` | Definition management and confirmed deletion |
| `/workspace/settings` | Revision-checked aggregate execution limits |

`/crawler/*`, `/dashboard/*`, and legacy dashboard routes return 404. There is no WebSocket subscription listener. Elasticsearch indexing, log search, and scoped log deletion remain supported by the pipeline and gateway interfaces.

See [workspace](workspace.md) for persistence, credentials, log deletion, and migration semantics. Synthetic requests and real-server fixtures are in `web/e2e` and `application` tests.
