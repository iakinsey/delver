# Delver crawl UI

The crawl workspace uses React 18, Vite, Vitest, and Playwright. Use Node 22.22 or newer.

```sh
npm ci --legacy-peer-deps
npm start
npm test
npm run build
npx playwright install chromium
npm run test:browser
```

Build output is `web/build`. Run `node scripts/package-ui.mjs` from the repository root after building to update the `web/assets` bundle embedded by Go. `make ui` does both steps. The default application serves the workspace UI and API together; see [the workspace guide](../docs/workspace.md). For separate static hosting, fall back to index.html.

Set `VITE_API_SERVER` before startup/build when using a separate API. Legacy `REACT_APP_API_SERVER` remains accepted; the Vite-prefixed value takes precedence. The default API port is 8080. The crawl UI uses HTTP API requests and does not open dashboard WebSocket subscriptions.

The dashboard builder, widget canvas, templates, and their dependencies have been removed. Old `/dashboards` and `/dashboard/:id` links redirect to the crawl workspace. Existing backend dashboard APIs and stored data are preserved for API clients.

## Crawl workspace verification

`npm run test:browser -- --workers=4` runs the full Chromium suite. The crawl matrix uses a separate local Go workspace on port 18083; the original workspace regression fixture uses 18081. Both use disposable storage, local pages/feeds, and authenticated HTTP and SOCKS5 proxies. No external crawl targets or paid proxy service are involved.

| Coverage | Cases | What is checked |
| --- | ---: | --- |
| Guided configuration cross-product | 216 | All 4 strategies × 3 scopes × 3 source modes (URLs, feeds, both) × 2 completion modes × 3 connections; save, reload, and editor round trip. Pacing, limits, and valid retention modes rotate through these cases. |
| Fetch configuration cross-product | 864 | Both concurrency modes × strategies × scopes × sources × connections × robots settings × completion modes; all generated fields save and reload. |
| Fetch execution cross-product | 48 | Both modes × connections × robots × phase inheritance × retry allowances; initiation, metrics, caps and completion. |
| Fetch nondefault round trips | 4 | Every setting changed and saved/reloaded in both modes with inherited/explicit phase timeouts. |
| Expert fetch controls | 4 | Both modes with/without an existing fetch policy; explicit Apply, worker counts, and opaque large-integer preservation. |
| Fetch pressure recovery | 2 | Both modes hold admission at a configured memory threshold, then start a corrected replacement while retaining history. |
| Fetch mobile accessibility | 2 | Both modes, all settings expanded, live metrics/table, no page overflow, and axe accessibility checks at 390px. |
| Fetch option boundaries | 80 component cases + browser checks | Every option in both modes: controls, min/max, empty values and phase inheritance; backend bounds and cross-field validation. |
| Executing crawl cross-product | 24 | Every strategy × connection × completion mode starts and processes real pages. All source and scope values execute; monitoring also exercises pause, resume, stop, failure inspection, and stored counters. |
| Actual traversal restrictions | 12 | Each scope × depth 1/2 × site hops 1/2, with inclusion/exclusion patterns, redirects escaping scope, subdomains, robots denial, and cross-site links. |
| TLS network paths | 4 | HTTPS pages directly, through HTTP CONNECT, and through SOCKS5; authenticated HTTPS proxy transport. Fixture certificates use an isolated CA file in the fixture process; production certificate verification is unchanged. |
| Generic network controls | 24 | Proxy requirement × hostname restriction × session labels, remote SOCKS hostname forwarding, shared labels and new-run separation, blocked feed/discovery/robots/redirect escapes, timeout/retry behavior, cancellation, editor persistence, and invalid combinations. |
| Robots policy | 6 | Respect/ignore across direct, HTTP proxy, and SOCKS; editor persistence, denied pages, feed crawling, and no robots requests when disabled. |
| Crawl deletion | 8 | Confirmation/cancel, preserve-logs choice, draft/running/archived deletion, API confirmation requirement, lost-response recovery, and persistence after reload. |
| Retention constraints | 8 | Every combination of the three keep-forever checkboxes, including invalid combinations rejected before saving. |
| Input and preview boundaries | Parameterized | Empty/invalid URLs, numeric bounds and fractions, durations, regular expressions, proxy formats, schemes/ports/subdomains, feed-only previews, stale and delayed preview responses. |
| Monitoring and lifecycle | Parameterized | Request/time limits separately and together, paused active time, continued discoveries, source health, archive/restore, independent crawls, drafts, and isolated historical results. |
| Queue recovery | Real failing sink | More than 50 dead letters, both page controls, retry, automatic refresh, page-size boundaries, and custom sink messaging. |
| Expert and failure recovery | Parameterized | Invalid JSON, opaque large integers, unsupported finite/budget pipelines, retained proxy credentials, conflicting editors, initialization failure and repair, and response loss after accepted create/start/draft/activation operations. |

The suite also covers mobile layout/accessibility, connection recovery, legacy crawler controls, and removal of dashboard navigation/routes. These are selected categorical and boundary cases, not a claim to enumerate every possible URL, regex, duration, worker plugin, or numeric value. Firefox/WebKit, real external proxy providers, and manual assistive-technology checks remain separate checks.
