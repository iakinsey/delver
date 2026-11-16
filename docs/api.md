# API and subscriptions

## Start services

The news example enables the JSON API on port 8080 and WebSocket subscriptions on port 26548, with Elasticsearch at `http://localhost:9200`. Set API database paths and both transformer and streamer search addresses explicitly for deployment.

Start the example site, Elasticsearch, and the application:

```sh
go run ./examples/site
./build/delver examples/news.json
```

See [the crawl UI README](../web/README.md) for development/build commands. The UI dashboard builder has been removed; dashboard storage APIs remain available to existing API clients. Configure trusted network access and TLS at the deployment boundary; the development API uses HTTP and the UI and both APIs provide direct access.

## JSON API

Send JSON request bodies. Successful responses have `http_code: 200`, `success: true`, and optional `data`. Failures have `success: false`, an application error code, and an `error` string. No credentials or account setup are required.

| Route | Request / result |
| --- | --- |
| `/dashboard/save` | `{ "id": "optional", "name": "...", "description": "...", "value": {} }`; returns the saved dashboard, including its ID |
| `/dashboard/load` | `{ "id": "..." }`; returns shared dashboard |
| `/dashboard/delete` | `{ "id": "..." }`; deletes shared dashboard |
| `/dashboard/list` | `{}`; lists shared dashboards |

Dashboard writes return persistence errors through the error response. Request bodies are limited to 1 MiB.

## WebSocket subscriptions

Connect to `ws://HOST:26548/` (use `wss` behind TLS). Dashboards and collected data are shared throughout the instance. Configure `streamer.allowed_origins` with exact dashboard origins, for example `["http://localhost:5173"]`. With no allowlist, only same-origin connections are accepted. Missing origins are rejected.

The version 1 protocol uses JSON frames:

```json
{"version":1,"type":"subscribe","id":"headlines","filter":{"data_type":"composite","query":{},"options":{"preload":true}}}
{"version":1,"type":"remove","id":"headlines"}
```

The server sends `ready` immediately. Clients may subscribe as soon as the connection opens. Historical search requests have a 15-second deadline. Each connection accepts at most 100 subscriptions and 64-KiB incoming frames. Updating an ID replaces only that subscription.

Messages retain `type` and `data`; each data item carries the subscription ID in `callback`. Control types are `subscribed`, `preload_complete`, `removed`, `error`, and `history_limit`. Errors include a `message`. Historical composite results return the latest 10,000 matching documents in chronological order; reaching that ceiling emits a history-limit warning. Browser charts retain their latest 5,000 timestamps, feeds their latest 250 entries, and maps deduplicate their latest 10,000 request IDs.


Composite query fields are `keyword`, `country`, `company`, `url`, `domain`, `http_code`, `title`, and `language`. URL refers to the document's `uri`, not its outgoing links; domain refers to `host`, including a nondefault port. URL/domain/status/title/language lists use OR. Country codes follow the supplied ISO alpha-3 dataset (`USA`, `GBR`, etc.). Every specified country, company, and case-insensitive literal keyword must match. Keywords search title and text. `range` is the lookback in days, default 90. `fields` selects output fields rather than requiring their presence.

Metric queries have `key`, `start`, and `end`, using Unix seconds. Omitted dates use a 90-day lookback through the current time. Historical metric preload supports `sum` and `avg` with `agg.time_window_seconds`. Responses convert Elasticsearch date buckets back to Unix seconds.

Aggregation configuration has `agg_name`, `time_field`, `agg_field`, and `time_window_seconds`. Live aggregation state persists across batches, with Unix-aligned half-open windows. Numeric fields can be nested dot paths. A window emits when a later sample crosses its boundary. Empty windows are omitted, and samples older than an already closed window are ignored. Preload includes the current open window and seeds it for subsequent live samples. Metric aggregate responses include `sample_count` so live averages preserve historical weighting. Pending windows are flushed when the server closes a subscription; delivery cannot be guaranteed after a remote peer has disconnected.

### Dataset index selection

Optional `streamer.composite_index` and `streamer.metric_index` select the historical dataset; empty values preserve the `composite` and `metric` defaults. All connections share that dataset. Live publications are selected by data type and subscription filters. The integration fixture uses isolated index names so tests cannot overwrite application data.

## Default dashboard

A fresh instance provisions one shared Overview dashboard. Dashboard IDs, names, widgets, and layouts are preserved when migrating an older database into the global collection. Ownership columns and per-account provisioning tables are removed transactionally. Intentional dashboard deletions remain deleted across restarts.

The default Overview begins with a Crawl log widget. `/crawler/log` returns the latest 500 fetch events for the current process, including URLs, timestamps, HTTP statuses, and failures. Existing Latest headlines defaults are upgraded at API startup; customized widget names and unrelated widgets are preserved.
