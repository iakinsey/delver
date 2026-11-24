# Repeated pages

Repeated-page families are a reversible scheduling and graph policy. They never
exclude sites, discard discoveries, label content malicious, or trigger a purge.
Trap exclusion and workspace protection remain separate policies.

## Configuration

Guided definitions use `repeated_pages`. Omitted settings resolve to the checked-in
defaults in `repetition/testdata/defaults.json`, including `deprioritize` mode.
Custom pipelines remain off when the frontier resource omits the settings. An
explicit opt-in requires a connected built-in composite extractor with `url`,
`text`, and `adversarial` features and a frontier accumulator. Expert settings are
patched on the server; opaque parameters and large JSON integers are retained.

The crawl editor's **Repeated pages** section exposes:

| Setting | Default | Allowed values |
| --- | --- | --- |
| Mode | Deprioritize | Off, Observe, Deprioritize |
| Independent canonical sites | 3 | 3–family window |
| Structure similarity | 0.95 | 0.5–1 |
| Visible-text similarity | 0.85 | 0.5–1 |
| Outgoing-destination similarity | 0.8 | 0.5–1 |
| Host family dominance | 0.8 | 0.5–1 |
| Pages per site | 20 | 3–200 |
| Pages per family | 100 | site window–1000 |
| Visible text limit | 256 KiB | 4–1024 KiB |
| Distinct outgoing destinations | 256 | 16–4096 |
| Repeated admission share | 10% | 1–50% |

Profiles use existing protected extraction output. Structural fingerprints ignore
attribute values and sibling ordering. Visible text uses Unicode NFKC normalization,
address masking, and bounded five-word shingle sketches. Substantive words and
numbers remain. Outgoing destinations are canonical sites. All three similarities
must meet their thresholds; structure or graph enclosure alone is insufficient.
Oversized or unavailable inputs are unclassified, with coverage reasons recorded.
The profile bounds do not truncate an input into an apparently matching profile.

Family confirmation requires evidence from three canonical sites. Subdomains of
one registrable domain or onion service count as one site. Page membership and
host dominance are distinct. Folding requires one confirmed, non-exempt family
to account for the configured fraction of retained eligible host observations.

## Scheduling and storage

Fresh frontier work and staged receipt chunks have normal and repeated routing
classes. Each class keeps its existing traversal order. The repeated share applies
when both classes have runnable work; capacity can be borrowed. Retry and recovery
shares, pacing, permits, real depth/hops, and sequence values retain their roles.
Explicit seeds and independent normal provenance remain normal. Provenance uses
compact durable observation IDs; source edges remain in the raw report graph.
There is no URL-level staging deduplication index.

Routing indexes migrate in bounded transactions with persistent seek cursors.
Startup does not wait for the complete pass, and prioritization begins after the
required indexes are ready. Exemptions, changed host dominance, and disabling the
policy reclassify pending work through the same bounded path. Configuration
inspection displays routing progress. **Finish restoration with the policy Off
before downgrading to a binary that predates this feature.**

Classification commits in the frontier database. A durable, revisioned outbox
projects compact family and host metadata into the existing report database;
replays are idempotent. Profiles retain hashes and measurements, not additional
page bodies. Retained evidence is bounded per site/family. Unreferenced provisional
prototypes are removed when their observation windows expire.

## Reports, analysis, and graphs

Reports contains searchable family inspection. Crawl configuration contains
**Exempt**, **Re-enable**, and **Analyze stored results**. Mutations use the existing
revision-checked operation journal. Analysis reads successful saved JSONL analyses
without fetching pages, checkpoints its byte position, and supports cancellation
and resumption. Missing, malformed, oversized, and protected records are counted.
An interrupted operation can resume from its last checkpoint. Rewritten result
files restart the scan with idempotent replay. Newer live observations take
precedence over older history.

Graph generation's **Fold repeated-page families** option defaults off. Raw input
limits and additional membership memory are checked before aggregation. Eligible
hosts are folded before clustering, layout, authority, and bridge calculations.
Displayed connections are deduplicated; original multiplicities and internal edge
counts remain available. Search resolves member names to their displayed family;
a bounded member panel shows individual metrics. Coordinates and membership belong
to the generated artifact. Changing folding mode requires regeneration. Existing
snapshots and raw graph mode retain their behavior.

Workspace protection removes the new metadata and invalidates generated artifacts.
Restricted-content propagation always follows the raw canonical-site graph, never
family membership or folded connections.

## Validation

`go test ./frontier ./repetition ./report ./application -run TestRepeated` selects
the feature regressions. `npm --prefix web run test:browser -- workspace-repeated.spec.js`
selects the browser cases. The complete browser inventory includes these cases.
`make test-million` and `make test-million-disk` extend the existing million-URL
lifecycle with legacy routing upgrades, interrupted migration, and exemption /
re-enablement at scale. The existing one-hour soak, graph scale, and isolated
Elasticsearch targets remain runnable through the Makefile. No Tor is needed.

The held-out corpus acceptance threshold is at least 95% replicated-member recall
and zero independent-content controls deprioritized. Those corpus results do not
guarantee perfect classification of arbitrary websites. Legitimate mirrors may
form families; this classification has no malicious-content meaning.

For a custom-pipeline downgrade, also remove the `repeated_pages` parameter from
frontier resources and apply that pipeline; the previous binary cannot validate
an unfamiliar resource parameter. Wait for `downgrade_ready: true` again after
applying it. Guided definitions can retain the disabled optional field. Do not
attempt a downgrade while reclassification or restoration remains pending.

Host dominance uses only that raw host's observations within the retained
canonical-site window. Sibling hosts do not inherit each other's membership.
Canonical identity still controls independent-site confirmation and shared site
budgets. A custom pipeline with multiple frontiers is unsupported when this
policy is enabled; preparation rejects that combination rather than merging
independent projection journals.
