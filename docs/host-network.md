# Generated host network

Open **Graph** in the shared Crawl / Reports / Graph navigation, or **Reports → Interactive network**. **Generate** creates a run-scoped snapshot; **Regenerate** includes later discoveries. Generation does not restart or reconfigure a crawl.

Defaults include visited hosts only and exclude hosts with no remaining connections after filtering. Visited means at least one recorded HTTP response, including HTTP errors. Nodes represent hostnames, without merging subdomains or registrable domains. Partial historical link coverage remains marked.

**Hide trap-excluded hosts** is enabled by default under **Generation parameters**, alongside the other host filters. Generation omits the selected run's active trap exclusions, including their subdomains and incident edges, before clustering and layout. The snapshot reports the number of omitted hosts. This changes the derived visualization, not crawl records or report totals.

Full and filtered snapshots are stored separately. Switching the checkbox selects the corresponding snapshot; generating one does not replace the other. Existing full snapshots remain available with the filter off. Generate a filtered snapshot when none exists, and use **Regenerate** after trap exclusions change or a site is restored. **Reload snapshot** loads an already generated snapshot; it does not recompute the filter.

## Geometry and zoom

The server computes weighted Louvain modularity communities on the undirected projection, with cached community totals and neighbor weights, recursively refines oversized communities, and lays out their weighted quotient and internal links with Barnes–Hut Fruchterman–Reingold forces. Disconnected components are laid out separately and packed by their actual footprints. Structurally equivalent leaves receive deterministic separation to avoid artificial rings. The analysis-pass parameter bounds local modularity passes, PageRank, and layout refinement; modularity aggregation has a 16-level ceiling. Directed links and PageRank retain their original directions. A structurally indivisible oversized community has bounded display partitions.

Each host has one immutable position, identity, community, and PageRank in the snapshot. Zooming and panning transform those coordinates; they never regroup hosts into grid cells, move community centers, or assign representative colors to mixed communities.

The server rasterizes every included host and directed link into 512-pixel image tiles. A precomputed whole-network overview remains underneath finer tiles while they load. Higher zoom levels reveal the same geometry at higher resolution. Edge bounding-box indexes retain links that cross a viewport even when both endpoints are outside it. Clustering and coloring are based on the network, not the tile grid. Neutral edge opacity increases with zoom; selecting a host highlights incoming links blue and outgoing links orange, with direction markers. Reciprocal highlights use parallel offsets.

Picking uses the same spatial index and coordinates, so a rendered host remains inspectable even when it is outside the bounded vector hit-testing overlay. Host details include hostname, visited/known URL counts, incoming/outgoing degrees, PageRank authority, and distinct neighboring communities (not shortest-path betweenness). Neighbor lists contain 200 entries per page. Search returns at most 20 hosts ranked by directed degree.

## Bounds and persistence

One generation runs at a time per process. Defaults: 200,000 hosts, 1,000,000 links, 512 MiB estimated generation working set, 12 analysis passes, maximum community size 800. Configurable ceilings: 2,000,000 hosts, 10,000,000 links, 4,096 MiB, 40 passes, community size 5,000. Limits fail explicitly without publishing a truncated snapshot. The memory estimate is not an OS memory limit. Jobs have a 30-minute deadline and can be cancelled. Failed/cancelled regeneration preserves the previous snapshot.

A completed temporary artifact atomically replaces the previous snapshot. Crawl deletion cancels and joins generation before deleting data. Artifacts use private permissions and remain in the run directory. Format 2 adds fixed-geometry raster indexes; older derived graph snapshots require regeneration. Crawl state and the report ledger require no migration.

The browser never downloads the full graph. Viewport counters return no host/edge arrays; only the selected host receives a client marker. The optional bounded inspection API still supports up to 5,000 real hosts / 20,000 links. Pixel rendering is independent of those overlay limits. Up to 37 tiles are requested per viewport including the overview, using three request workers. The decoded-image cache holds at most 48 tiles; zooming out discards finer levels from rendering. Obsolete requests are aborted. Canvas device pixel ratio is capped at 2 and backing dimensions at 4,096 pixels. Host markers have a saturating authority scale capped at 8 screen pixels.

Server tile rendering has two slots, a 32 MiB / 256-entry PNG cache, streaming indexed queries, and 20-second request deadlines. Selected-host tiles are not retained in the shared PNG cache. These bounds do not grow with the total host count.

Endpoints use the existing POST envelope and crawl/run identifiers: `atlas_generate`, `atlas_cancel`, `atlas_status`, `atlas_view`, `atlas_tile`, `atlas_pick`, `atlas_host`, `atlas_search`. Snapshot identities are checked before reading geometry. **Reload snapshot** loads a snapshot regenerated by another client.

## Validation

`go test ./report -run Atlas` covers filtering, real-node coordinate invariance across zoom, modularity communities, geometry continuity across tiles, crossing edges, both highlight directions, picking, limits, cancellation, and atomic replacement. `go test ./application -run Atlas` covers API lifecycle and persisted snapshots. Frontend units cover tile/camera transforms and request bounds. `cd web && npx playwright test workspace-atlas.spec.js` exercises generation, selection, search, highlights, pan/zoom, limits, navigation, and mobile layout against a real backend.

`make test-network-million` starts with one million synthetic visited hosts and roughly two million directed edges. It generates the complete snapshot, checks exact counts, bounded overlay queries, generation heap budget, and detailed viewports. The browser million-host case also checks bounded heap and interactive navigation. Evidence remains outside Git. The original frontier scale workloads are separate.
