# Configuration reference

An application JSON object has `config`, `resources`, and `workers`. Names must be unique within each collection. Built-in parameter fields and global configuration are checked strictly; misspellings are errors. Resource references are names, not nested objects. Validation checks queue wiring and built-in resource interface compatibility before startup.

Durations accept Go strings such as `"250ms"`, `"10s"`, or `"1h"`; numeric durations are nanoseconds. `max_poll_delay_ms` is an integer number of milliseconds. All paths are resolved relative to the current working directory unless stated otherwise.

## Global settings

| Section / field | Default / meaning |
| --- | --- |
| `workers.enabled` | `true`; enable worker execution |
| `workers.worker_counts` | CPU count × 8; default execution loops per ordinary worker; examples use 4 |
| `shutdown_timeout` | `30s` |
| `http_client.timeout` | `10s` per HTTP request |
| `http_client.user_agent` | `delver pre-alpha` |
| `http_client.max_retries` | 1 retry after the first attempt; range 0–10; transport errors, 429, and 5xx |
| `http_client.retry_delay` | `100ms`; initial exponential retry delay |
| `http_client.retry_max_delay` | `5s`; backoff cap, except explicit `Retry-After` |
| `http_client.max_response_bytes` | 16777216; maximum stored page body and parsed feed body |
| `http_client.per_origin_concurrency` | 2 |
| `http_client.request_delay` | `100ms` between scheduled starts per origin |
| `http_client.respect_robots` | `true`; deny when robots retrieval/parsing fails |
| `http_client.allowed_domains` | Empty: unrestricted; otherwise allow matching hostnames or registrable domains |
| `http_client.socks5_url` | Optional `host:port`, `socks5://`, or `socks5h://` URL; destination hostnames are resolved by the proxy |
| `http_client.http_proxy_url` | Optional HTTP/HTTPS proxy URL; configuring both proxy types is rejected |
| `http_client.require_proxy` | `false`; reject startup/requests without an explicit proxy |
| `http_client.allowed_host_suffixes` | Empty: unrestricted; otherwise exact hostname or dot-boundary suffix matches, enforced on all built-in requests including robots and redirects |
| `http_client.isolate_proxy_sessions` | `false`; generated per-run SOCKS authentication labels for proxies supporting session isolation; incompatible with supplied proxy login credentials |
| `robots.expiration` | `1h`; cached by scheme and host, checked on access |
| `robots.clear_expired_delay` | `1h`; periodic cleanup; lookup expiry also enforces freshness |
| `api.enabled`, `api.address` | `true`, `:8080`; examples disable the API unless needed |
| `api.allow_cors` | `true` |
| `streamer.enabled` | `false`; enabled legacy streaming destinations return an explicit migration error |
| `metrics.enabled` | `true`; metrics output requires a queue named `transformer_queue` |
| `persistent_map.gc_interval` | `5m` |
| `persistent_map.gc_discard_ratio` | `0.7`, strictly between 0 and 1 |
| `persistent_map.gc_err_threshold` | 2 consecutive real errors before maintenance stops; no-rewrite results do not count |
| `persistent_map.default_prefetch_size` | 64 |
| `countries_path`, `companies_path` | Data files beside the executable; set explicitly for deployments using mention extraction |
| `adversarial.subdomain_threshold` | 25 distinct subdomains |
| `adversarial.enumeration_threshold` | 1 |
| `default_save_interval` | `2m`; default persistent rolling-Bloom save interval, overridden by its explicit `save_interval` |

Proxy session labels are shared by workers in a runtime, stable for the same run storage path, and different for new runs. They are private runtime state, not a configurable session key. The proxy controls actual isolation. Guided crawl settings and an external SOCKS listener example are described in [the workspace guide](workspace.md#private-namespaces-and-slower-networks). Custom workers must use the runtime HTTP configuration to inherit its restrictions.

Retries use capped exponential backoff and honor `Retry-After`. A fetch's `success` means transport and storage succeeded; examine `http_code` separately. The effective URL is retained after redirects.

## Resources

Every resource has `name`, `type`, and a `parameters` object.

| Type | Parameters |
| --- | --- |
| `crawl_queue` | Required `path`, `crawl_id`; `max_attempts` defaults to 3; `recrawl_after` defaults to disabled. Accepts only fetch requests. Exact persistent deduplication. |
| `file_queue` | Required `name`, `path`, `dlq_path` (distinct); optional `max_poll_delay_ms`, `max_size` (0 = unlimited), `max_attempts` (factory default 3), `reset` (enable claimed-record recovery), `resilient` (factory default true). Lower numeric priority runs first. |
| `channel_queue` | No parameters. Unbuffered transient queue, no retry persistence. |
| `timer` | Positive `delay`. Produces immediate and periodic timer messages; jobs normally create their own timer. |
| `filesystem_object_store` | Required `path`. Atomic body files. |
| `persistent_map` | Required `path`. Badger byte-key/value map. |
| `multi_host_map` | Required `base_path`. One Badger directory per registrable domain. |
| `bloom_filter` | Positive `max_n`, probability `0 < p < 1`. In-memory approximate membership. |
| `rolling_bloom_filter` | `max_n`, `p`, positive `bloom_count`, optional `path`, `save_interval`. Persists every retained generation. |

Use `crawl_queue` for request deduplication. Bloom filters are opt-in approximations: false positives can omit unseen pages. `p: 0.001` is a reasonable example; `p: 1` is invalid.

A queue directory has one exclusive consumer owner. Do not share file or Badger directories between processes. The runtime starts queue pumps for configured worker inboxes; terminal output queues remain readable without a consumer claiming their records.

## Workers

Common fields: `name`, `type`, `inbox`, `outbox`, `count`, `manager`, `interval`, `parameters`. Omitted manager and `"worker"` both mean ordinary queue consumption. `"job"` supplies periodic timer messages, requires a positive interval, and defaults to one execution loop. Ordinary workers use the configured global count when `count` is omitted.

| Type | Parameters and behavior |
| --- | --- |
| `fixed_seed_publisher` | `uris`: HTTP/HTTPS seeds at depth 0; normally a job |
| `rss_feed_publisher` | `uris`: RSS/Atom feeds; optional `max_concurrency` (default 8); emits article requests at depth 1; normally a job |
| `http_fetcher` | Resource reference `object_store`; global HTTP settings control retries and policy |
| `composite_extractor` | `object_store`, `enabled` extractor names; optional `timeout` (default `30s` per dependency stage) and `transformer_queue` output reference, defaulting to that resource name if present |
| `dfs_basic_accumulator` | `url_store` map, `max_depth`; optional `visited_urls` Bloom resource. Same-domain children return to its outbox; external domains are persisted. Omit the approximate filter with an exact crawl queue. |
| `dfs_basic_publisher` | `output_queue`, `url_store_path`, `visited_hostmap`, `rotate_after`; normally a job. Publishes one stored domain after the request queue remains empty long enough. |
| `news_accumulator` | `news_queue`; optional `seen_urls`, `max_depth` (default 1). Publishes article analyses and filters discovered links with news heuristics. |
| `transformer` | `enabled` values `composite` and/or `metric`; `search_addresses`. Indexes accepted messages synchronously. |
| `jsonl_sink` | `path`. Appends message envelopes and synchronizes writes. No outbox. |

Publishing workers require an outbox. `dfs_basic_publisher` publishes directly to its `output_queue`; transformers and sinks terminate their message path.

## Extractors

| Name | Required enabled extractors | Output |
| --- | --- | --- |
| `url` | None | Resolved HTTP/HTTPS links |
| `text` | None | Tag-stripped text with normalized whitespace |
| `title` | None | HTML title |
| `language` | `text` | ISO language name and confidence |
| `country` | `text` | Country codes from configured mention patterns |
| `company_name` | `text` | Company identifiers from configured names |
| `ngram` | `text` | Lowercased trigrams |
| `adversarial` | `url` | Enumeration and subdomain-explosion indicators |
| `sentiment` | `language`, `title` | English title sentiment |

Dependencies must be listed explicitly. Unknown, duplicate, missing, or cyclic dependencies fail validation.

### Retired services

Dashboard CRUD and streaming subscriptions are removed. Configure Elasticsearch indexing through transformer workers. Workspace execution limits are configured once through `/workspace/settings`; per-crawl policies retain local concurrency, pacing, retry, admission, and metric controls.
