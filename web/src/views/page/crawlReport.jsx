import React, { useState, useEffect, useRef, useCallback } from "react";
import { useHistory, useLocation, useParams } from "react-router-dom";
import { apiRequest } from "./crawlApi";
import { HostGraph } from "./hostGraph";
import "./workspace.css";
import "./report.css";
const number = (v) =>
  Number(v || 0).toLocaleString(undefined, { maximumFractionDigits: 2 });
const call = (action, data, signal) =>
  apiRequest(`crawls/${action}`, data, { signal, timeoutMs: 35000 });
export default function CrawlReport() {
  const { crawlId } = useParams(),
    location = useLocation(),
    history = useHistory();
  const runId = new URLSearchParams(location.search).get("run_id") || "";
  const [summary, setSummary] = useState(null),
    [error, setError] = useState(""),
    [query, setQuery] = useState(""),
    [matches, setMatches] = useState([]),
    [after, setAfter] = useState(""),
    [active, setActive] = useState(-1),
    [open, setOpen] = useState(false),
    [suggest, setSuggest] = useState(false),
    [host, setHost] = useState(""),
    [depth, setDepth] = useState("2"),
    [mode, setMode] = useState("hosts"),
    [limits, setLimits] = useState({ node_limit: 500, edge_limit: 2000 }),
    [draftLimits, setDraftLimits] = useState(limits),
    [expanded, setExpanded] = useState([]),
    [view, setView] = useState(null),
    [busy, setBusy] = useState(false),
    [graphError, setGraphError] = useState(""),
    [inspection, setInspection] = useState(null),
    [refresh, setRefresh] = useState(0);
  const explorer = useRef(null),
    graphVersion = useRef(0),
    searchVersion = useRef(0);
  const data = { crawl_id: crawlId, run_id: runId };
  useEffect(() => {
    let live = true,
      timer;
    const controller = new AbortController();
    setSummary(null);
    setError("");
    async function poll() {
      try {
        const r = await call(
          "report",
          { crawl_id: crawlId, run_id: runId },
          controller.signal,
        );
        if (live) {
          setSummary(r);
          setError("");
        }
      } catch (e) {
        if (live) setError(e.message);
      }
      if (live) timer = setTimeout(poll, 2000);
    }
    poll();
    return () => {
      live = false;
      clearTimeout(timer);
      controller.abort();
    };
  }, [crawlId, runId]);
  useEffect(() => {
    if (!open || !suggest) return;
    const controller = new AbortController();
    const version = ++searchVersion.current;
    const timer = setTimeout(() => {
      call("hosts", { ...data, query, limit: 20 }, controller.signal)
        .then((rows) => {
          if (version === searchVersion.current) {
            setMatches(rows);
            setAfter(rows.at(-1)?.cursor || rows.at(-1)?.name || "");
            setActive(-1);
          }
        })
        .catch((e) => {
          if (!controller.signal.aborted) setGraphError(e.message);
        });
    }, 250);
    return () => {
      clearTimeout(timer);
      controller.abort();
      searchVersion.current++;
    };
  }, [query, open, suggest, crawlId, runId]);
  useEffect(() => {
    if (!host) return;
    const d = Number(depth);
    if (!/^\d+$/.test(depth) || !Number.isSafeInteger(d) || d > 2147483647) {
      setBusy(false);
      setGraphError("Depth must be a nonnegative integer");
      return;
    }
    const controller = new AbortController(),
      version = ++graphVersion.current;
    setBusy(true);
    setGraphError("");
    const timer = setTimeout(() => {
      call(
        "host_graph",
        { ...data, host, depth: d, mode, ...limits, expanded },
        controller.signal,
      )
        .then((r) => {
          if (version === graphVersion.current) {
            setView(r);
            setInspection(r.nodes.find((n) => n.selected) || null);
          }
        })
        .catch((e) => {
          if (version === graphVersion.current && !controller.signal.aborted)
            setGraphError(e.message);
        })
        .finally(() => {
          if (version === graphVersion.current) setBusy(false);
        });
    }, 200);
    return () => {
      clearTimeout(timer);
      controller.abort();
      graphVersion.current++;
    };
  }, [host, depth, mode, limits, expanded, refresh, crawlId, runId]);
  useEffect(() => {
    setQuery("");
    setHost("");
    setMatches([]);
    setView(null);
    setInspection(null);
    setExpanded([]);
    setOpen(false);
    setSuggest(false);
    setGraphError("");
    setBusy(false);
  }, [crawlId, runId]);
  const pick = useCallback((n) => setInspection(n), []),
    renderError = useCallback((message) => setGraphError(message), []);
  function choose(name) {
    setHost(name);
    setQuery(name);
    setSuggest(false);
    setMatches([]);
    setExpanded([]);
    setInspection(null);
    setOpen(true);
  }
  async function more() {
    const v = searchVersion.current;
    try {
      const rows = await call("hosts", { ...data, query, after, limit: 20 });
      if (v === searchVersion.current) {
        setMatches((old) => [...old, ...rows]);
        setAfter(rows.at(-1)?.cursor || rows.at(-1)?.name || "");
      }
    } catch (e) {
      setGraphError(e.message);
    }
  }
  async function fullscreen() {
    try {
      if (document.fullscreenElement) await document.exitFullscreen();
      else await explorer.current.requestFullscreen();
    } catch (e) {
      setGraphError(e.message);
    }
  }
  const oversized =
    view &&
    (view.total_hosts > limits.node_limit ||
      view.total_edges > limits.edge_limit);
  return (
    <main className="crawl-workspace crawl-report">
      <header className="crawl-heading">
        <h1>Report</h1>
        <button onClick={() => history.push(`/crawls/${crawlId}`)}>
          Crawl
        </button>
      </header>
      {error && (
        <p role="alert" className="crawl-error">
          {error}
        </p>
      )}
      {!summary && !error && <p role="status">Loading…</p>}
      {summary && (
        <>
          <div className="report-cards" aria-label="Run report metrics">
            <div>
              <strong>{number(summary.requests_per_second)}</strong>
              <span>Requests/s</span>
              <small>
                {summary.rate_basis === "average"
                  ? "Average over running time"
                  : "Completed fetch attempts · 60s"}
              </small>
            </div>
            <div>
              <strong>{number(summary.counts.visited)}</strong>
              <span>URLs visited</span>
              <small>Unique URLs with an HTTP response</small>
            </div>
            <div>
              <strong>{number(summary.counts.not_visited)}</strong>
              <span>URLs not visited</span>
              <small>Discovered URLs without an HTTP response</small>
            </div>
          </div>
          {summary.backfill_error && (
            <p role="alert" className="crawl-error">
              {summary.backfill_error}
            </p>
          )}
          {summary.counts.partial && (
            <p className="report-coverage">
              Partial history
              {summary.backfill_pending ? " · Backfill running" : ""}
            </p>
          )}
          <p className="report-run">
            Run: {summary.run_id} · {summary.state}
          </p>
        </>
      )}
      <section
        className="crawl-panel report-explorer"
        ref={explorer}
        aria-label="Host explorer"
      >
        <div className="report-search-row">
          <label className="report-search">
            Search hosts
            <input
              role="combobox"
              aria-label="Search hosts"
              aria-expanded={suggest && matches.length > 0}
              aria-controls="report-host-matches"
              aria-autocomplete="list"
              aria-activedescendant={
                active >= 0 ? `report-host-${active}` : undefined
              }
              value={query}
              autoComplete="off"
              onFocus={() => {
                setOpen(true);
                setSuggest(true);
              }}
              onChange={(e) => {
                setQuery(e.target.value);
                setSuggest(true);
              }}
              onKeyDown={(e) => {
                if (e.key === "ArrowDown") {
                  setActive((i) => Math.min(i + 1, matches.length - 1));
                  e.preventDefault();
                } else if (e.key === "ArrowUp") {
                  setActive((i) => Math.max(i - 1, 0));
                  e.preventDefault();
                } else if (e.key === "Enter" && matches.length) {
                  choose(matches[Math.max(0, active)].name);
                  e.preventDefault();
                } else if (e.key === "Escape") {
                  setSuggest(false);
                  setActive(-1);
                }
              }}
            />
          </label>
          <label>
            Depth
            <input
              aria-label="Depth"
              type="number"
              min="0"
              max="2147483647"
              step="1"
              value={depth}
              onChange={(e) => setDepth(e.target.value)}
            />
          </label>
        </div>
        {suggest && open && (
          <div className="report-suggestions">
            <ul
              role="listbox"
              id="report-host-matches"
              aria-label="Matching hosts"
            >
              {matches.map((n, i) => (
                <li
                  key={n.id}
                  role="option"
                  id={`report-host-${i}`}
                  aria-selected={i === active}
                >
                  <button onClick={() => choose(n.name)}>
                    {n.name}
                    <small>
                      {number(n.connections)} connections · {number(n.visited)}{" "}
                      visited / {number(n.known)} URLs
                    </small>
                  </button>
                </li>
              ))}
            </ul>
            {!matches.length && <p>Empty</p>}
            {matches.length >= 20 && after && (
              <button onClick={more}>More hosts</button>
            )}
          </div>
        )}
        {open && host && (
          <>
            <div className="report-toolbar">
              <span>{host}</span>
              <button
                aria-pressed={mode === "hosts"}
                onClick={() => {
                  setMode("hosts");
                  setExpanded([]);
                }}
              >
                Hosts
              </button>
              <button
                aria-pressed={mode === "grouped"}
                disabled={!oversized && mode !== "grouped"}
                onClick={() => {
                  setMode("grouped");
                  setExpanded([]);
                }}
              >
                Grouped
              </button>
              <button onClick={() => setRefresh((n) => n + 1)} disabled={busy}>
                Refresh
              </button>
              <button onClick={fullscreen}>Fullscreen</button>
            </div>
            <details>
              <summary>Display settings</summary>
              <form
                className="report-limits"
                onSubmit={(e) => {
                  e.preventDefault();
                  const n = Number(draftLimits.node_limit),
                    l = Number(draftLimits.edge_limit);
                  if (
                    !Number.isInteger(n) ||
                    n < 10 ||
                    n > 2000 ||
                    !Number.isInteger(l) ||
                    l < 32 ||
                    l > 10000
                  ) {
                    setGraphError(
                      "Render limits: 10–2000 nodes, 32–10000 links",
                    );
                    return;
                  }
                  setLimits({ node_limit: n, edge_limit: l });
                  setExpanded([]);
                }}
              >
                <label>
                  Rendered nodes
                  <input
                    aria-label="Rendered nodes"
                    type="number"
                    min="10"
                    max="2000"
                    value={draftLimits.node_limit}
                    onChange={(e) =>
                      setDraftLimits({
                        ...draftLimits,
                        node_limit: e.target.value,
                      })
                    }
                  />
                </label>
                <label>
                  Rendered links
                  <input
                    aria-label="Rendered links"
                    type="number"
                    min="32"
                    max="10000"
                    value={draftLimits.edge_limit}
                    onChange={(e) =>
                      setDraftLimits({
                        ...draftLimits,
                        edge_limit: e.target.value,
                      })
                    }
                  />
                </label>
                <button>Apply</button>
              </form>
            </details>
            {busy && <p role="status">Calculating…</p>}
            {graphError && (
              <p role="alert" className="crawl-error">
                {graphError}
              </p>
            )}
            {view && (
              <>
                <p className="report-legend">
                  Size: authority · Amber ring: cross-community connector ·
                  Hollow: unvisited · Purple: host group
                </p>
                <HostGraph view={view} onPick={pick} onError={renderError} />
                <p>
                  {view.nodes.length} rendered / {number(view.total_hosts)}{" "}
                  hosts · {view.edges.length} rendered /{" "}
                  {number(view.total_edges)} directed links
                  {view.grouped
                    ? " · Grouped"
                    : view.sampled
                      ? " · Structural sample"
                      : ""}
                </p>
                <small>
                  Graph revision {view.revision} ·{" "}
                  {new Date(view.analyzed_at).toLocaleString()}
                </small>
                <div className="report-node-details">
                  {inspection && (
                    <>
                      <h2>{inspection.name}</h2>
                      {inspection.group ? (
                        <>
                          <p>
                            {inspection.members} hosts · Depth{" "}
                            {inspection.depth}–{inspection.max_depth} ·{" "}
                            {inspection.internal_links} internal host links
                          </p>
                          <button
                            onClick={() =>
                              setExpanded((x) => [...x, inspection.id])
                            }
                          >
                            Expand
                          </button>
                        </>
                      ) : (
                        <>
                          <p>
                            Authority percentile:{" "}
                            {number(inspection.percentile)} · Incoming:{" "}
                            {inspection.incoming} · Outgoing:{" "}
                            {inspection.outgoing} · Other communities:{" "}
                            {inspection.bridges}
                          </p>
                          <button onClick={() => choose(inspection.name)}>
                            Center
                          </button>
                        </>
                      )}
                      <p>
                        {number(inspection.visited)} visited /{" "}
                        {number(inspection.known)} URLs
                      </p>
                    </>
                  )}
                </div>
                <details>
                  <summary>Visible hosts and groups</summary>
                  <ul className="report-visible">
                    {view.nodes.map((n) => (
                      <li key={n.id}>
                        <button onClick={() => setInspection(n)}>
                          {n.name}
                        </button>
                        {!n.group && (
                          <span>
                            Authority {number(n.percentile)} · {n.bridges}{" "}
                            community connections
                          </span>
                        )}
                      </li>
                    ))}
                  </ul>
                </details>
              </>
            )}
          </>
        )}
        {graphError && !host && (
          <p role="alert" className="crawl-error">
            {graphError}
          </p>
        )}
      </section>
    </main>
  );
}
