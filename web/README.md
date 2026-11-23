# Delver crawl UI

The workspace uses React, Vite, Vitest, and Playwright. Use Node 22.22 or newer.

```sh
make test-setup
make build
make test
make test-ui
```

For development, run `npm start` in `web`. Set `VITE_API_SERVER` for a separate backend. The normal Go application embeds the UI and serves the workspace API on port 8080. Dashboard CRUD, singleton crawler routes, and streaming subscriptions are removed.

## Targeted browser coverage

The browser suite runs against automatically built real Go backends on dynamically assigned loopback ports, temporary storage, controlled pages/feeds, authenticated HTTP/SOCKS proxies, TLS, and deterministic fault resources. Playwright installs its managed Chromium; no machine-specific browser path is required.

`e2e/configurationManifest.mjs` declares 16 selected interaction configurations. Each is configured/saved/reloaded/edited/started through browser controls and checked for compiled settings, network behavior, progress, results, and termination. This is targeted interaction coverage, not a Cartesian product. Additional focused tests cover:

- Every guided/editor control, numeric/duration/text/URL/regex/credential boundaries, retention constraints, and previews.
- Direct/HTTP/SOCKS/TLS, proxy requirement and session isolation, robots, redirect/hostname restrictions, retries/timeouts, all traversal/source/scope/completion choices.
- Custom pipelines and large integers, fetch settings, historical snapshots/results, archive/restore, failed inspection/retry, both log-deletion options.
- Lifecycle transitions, pending/double commands, two-tab conflicts, response loss, initialization failure/repair, disconnection/reconnection, keyboard navigation, mobile/desktop and accessibility.
- Workspace limit changes across running crawls, independent local settings, persisted counters, overlapping pressure and same-run recovery, and actual backend process restart.

`tests/ui-coverage.json` records the required stable test titles and files. `make test-ui` publishes the expected count, imposes a 30-minute invocation limit, and rejects missing, duplicate, failed, skipped, or quarantined cases. The million-host graph case runs in its own backend process after ordinary cases, so scale seeding and layout do not compete with unrelated workspace mutations. Raw phase reports and an aggregate report retain every result; the inventory gate checks all cases together. Artifacts are isolated outside Git. A focused reproduction is runnable with `npm --prefix web run test:browser -- workspace-settings.spec.js --grep 'combined adaptive'`; acceptance still requires the complete final suite.

Unit, race, benchmark, one-hour soak, both million-URL tests, Elasticsearch and 30-minute Tor gates are separate and unchanged. See [validation commands](../docs/manual-testing.md).
