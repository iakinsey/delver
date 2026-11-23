import { useNetworkTiles, tileGeometry } from "./networkTiles";
import CrawlNavigation from "./crawlNavigation";
import React, { useCallback, useEffect, useRef, useState } from "react";
import { useLocation, useParams } from "react-router-dom";
import { apiRequest } from "./crawlApi";
import "./workspace.css";
import "./networkGraph.css";
const defaults = {
  max_hosts: 200000,
  max_edges: 1000000,
  memory_mb: 512,
  passes: 12,
  cluster_size: 800,
  visited_only: true,
  remove_orphans: true,
};
const fmt = (v) => Number(v || 0).toLocaleString();
const color = (n) => `hsl(${(((n * 137.508) % 360) + 360) % 360} 65% 65%)`;
export function clampCamera(c) {
  const clamp = (v, min, max, fallback) =>
    Number.isFinite(v) ? Math.max(min, Math.min(max, v)) : fallback;
  return {
    x: clamp(c.x, -0.5, 1.5, 0.5),
    y: clamp(c.y, -0.5, 1.5, 0.5),
    zoom: clamp(c.zoom, 0.5, 10000, 1),
  };
}
export function graphBounds(camera, width, height) {
  const unit = Math.min(width, height) * camera.zoom;
  return [
    camera.x - width / unit / 2,
    camera.y - height / unit / 2,
    camera.x + width / unit / 2,
    camera.y + height / unit / 2,
  ].map((v) => Math.max(-1, Math.min(2, v)));
}
export function nodeRadius(node, hosts) {
  return Math.min(
    8,
    1.4 + 6.6 * (1 - Math.exp((-node.authority * Math.max(1, hosts)) / 8)),
  );
}
export function NetworkCanvas({
  frame,
  tiles = [],
  onPickPoint,
  totalHosts,
  selectedNode,
  camera,
  setCamera,
  selection,
  onPick,
  onSize,
}) {
  const ref = useRef(null),
    drag = useRef(null),
    state = useRef({});
  state.current = { frame, camera, setCamera, selection, onPick, onPickPoint };
  useEffect(() => {
    const canvas = ref.current;
    const observer = new ResizeObserver(() => {
      const r = canvas.getBoundingClientRect();
      onSize({ width: r.width, height: r.height });
    });
    observer.observe(canvas);
    return () => observer.disconnect();
  }, [onSize]);
  useEffect(() => {
    const canvas = ref.current;
    const wheel = (e) => {
      e.preventDefault();
      const r = canvas.getBoundingClientRect(),
        old = state.current.camera,
        unit = Math.min(r.width, r.height) * old.zoom;
      const px = e.clientX - r.left - r.width / 2,
        py = e.clientY - r.top - r.height / 2;
      const zoom = Math.max(
        0.5,
        Math.min(10000, old.zoom * Math.exp(-e.deltaY * 0.002)),
      );
      state.current.setCamera({
        x: old.x + px / unit - px / (Math.min(r.width, r.height) * zoom),
        y: old.y + py / unit - py / (Math.min(r.width, r.height) * zoom),
        zoom,
      });
    };
    canvas.addEventListener("wheel", wheel, { passive: false });
    return () => canvas.removeEventListener("wheel", wheel);
  }, []);
  useEffect(() => {
    const id = requestAnimationFrame(() => {
      const canvas = ref.current,
        r = canvas.getBoundingClientRect(),
        dpr = Math.min(
          window.devicePixelRatio || 1,
          2,
          4096 / Math.max(r.width, r.height),
        );
      const width = Math.max(1, Math.round(r.width * dpr)),
        height = Math.max(1, Math.round(r.height * dpr));
      if (canvas.width !== width || canvas.height !== height) {
        canvas.width = width;
        canvas.height = height;
      }
      const ctx = canvas.getContext("2d");
      if (!ctx) return;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.fillStyle = "#090f19";
      ctx.fillRect(0, 0, r.width, r.height);
      for (const tile of tiles) {
        const rect = tileGeometry(tile, camera, r.width, r.height);
        if (
          rect.x + rect.size < 0 ||
          rect.y + rect.size < 0 ||
          rect.x > r.width ||
          rect.y > r.height
        )
          continue;
        ctx.drawImage(tile.image, rect.x, rect.y, rect.size, rect.size);
      }

      const unit = Math.min(r.width, r.height) * camera.zoom;
      const point = (n) => [
        (n.x - camera.x) * unit + r.width / 2,
        (n.y - camera.y) * unit + r.height / 2,
      ];
      const selected = selectedNode?.id === selection ? `h:${selection}` : null;
      for (const n of selected
        ? [{ ...selectedNode, id: selected, host_id: selection }]
        : []) {
        const [x, y] = point(n),
          radius = nodeRadius(n, totalHosts || frame.hosts);
        ctx.fillStyle = color(n.community);
        ctx.beginPath();
        ctx.arc(x, y, radius, 0, Math.PI * 2);
        ctx.fill();
        if (n.members > 1 || n.id === selected) {
          ctx.strokeStyle = n.id === selected ? "#ffffff" : "#adb7c0";
          ctx.lineWidth = n.id === selected ? 2 : 0.6;
          ctx.stroke();
        }
      }
    });
    return () => cancelAnimationFrame(id);
  }, [frame, tiles, camera, selection, selectedNode, totalHosts]);
  function pick(e) {
    const { frame, camera } = state.current;

    const r = ref.current.getBoundingClientRect(),
      unit = Math.min(r.width, r.height) * camera.zoom,
      x = (e.clientX - r.left - r.width / 2) / unit + camera.x,
      y = (e.clientY - r.top - r.height / 2) / unit + camera.y;
    state.current.onPickPoint?.({ x, y, radius: Math.min(0.1, 10 / unit) });
  }
  return (
    <canvas
      ref={ref}
      className="network-canvas"
      aria-label="Interactive host network"
      tabIndex={0}
      onPointerDown={(e) => {
        ref.current.setPointerCapture(e.pointerId);
        drag.current = {
          x: e.clientX,
          y: e.clientY,
          camera: state.current.camera,
          moved: false,
        };
      }}
      onPointerMove={(e) => {
        const d = drag.current;
        if (!d) return;
        const dx = e.clientX - d.x,
          dy = e.clientY - d.y;
        if (Math.hypot(dx, dy) > 3) d.moved = true;
        const r = ref.current.getBoundingClientRect(),
          u = Math.min(r.width, r.height) * d.camera.zoom;
        setCamera({
          ...d.camera,
          x: Math.max(-0.5, Math.min(1.5, d.camera.x - dx / u)),
          y: Math.max(-0.5, Math.min(1.5, d.camera.y - dy / u)),
        });
      }}
      onPointerUp={(e) => {
        if (drag.current && !drag.current.moved) pick(e);
        drag.current = null;
      }}
      onPointerCancel={() => {
        drag.current = null;
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          state.current.onPick(null);
          return;
        }
        if (
          [
            "+",
            "=",
            "-",
            "ArrowLeft",
            "ArrowRight",
            "ArrowUp",
            "ArrowDown",
            "Home",
          ].includes(e.key)
        ) {
          e.preventDefault();
          setCamera((c) =>
            e.key === "Home"
              ? { x: 0.5, y: 0.5, zoom: 1 }
              : {
                  x:
                    c.x +
                    (e.key === "ArrowRight"
                      ? 0.1 / c.zoom
                      : e.key === "ArrowLeft"
                        ? -0.1 / c.zoom
                        : 0),
                  y:
                    c.y +
                    (e.key === "ArrowDown"
                      ? 0.1 / c.zoom
                      : e.key === "ArrowUp"
                        ? -0.1 / c.zoom
                        : 0),
                  zoom: Math.max(
                    0.5,
                    Math.min(
                      10000,
                      c.zoom *
                        (e.key === "-"
                          ? 0.8
                          : ["+", "="].includes(e.key)
                            ? 1.25
                            : 1),
                    ),
                  ),
                },
          );
        }
      }}
    />
  );
}
export default function NetworkGraph() {
  const { crawlId } = useParams(),
    location = useLocation(),
    runId = new URLSearchParams(location.search).get("run_id") || "";
  const [status, setStatus] = useState(null),
    [options, setOptions] = useState(defaults),
    [hideTraps, setHideTraps] = useState(true),
    [error, setError] = useState(""),
    [statusError, setStatusError] = useState(""),
    [connectionVersion, setConnectionVersion] = useState(0),
    [busy, setBusy] = useState(false),
    [camera, setCameraInternal] = useState({ x: 0.5, y: 0.5, zoom: 1 }),
    [size, setSize] = useState({ width: 800, height: 650 }),
    [frame, setFrame] = useState(null),
    [selection, setSelection] = useState(null),
    [inspection, setInspection] = useState(null),
    [direction, setDirection] = useState("outgoing"),
    [offset, setOffset] = useState(0),
    [query, setQuery] = useState(""),
    [matches, setMatches] = useState([]);
  const setCamera = useCallback(
    (next) =>
      setCameraInternal((old) =>
        clampCamera(typeof next === "function" ? next(old) : next),
      ),
    [],
  );
  const pickRequest = useRef(null);
  const settingsFor = useRef(""),
    disconnected = useRef(false);
  const snapshot = status?.snapshot,
    running = status?.job?.state === "running";
  const call = useCallback(
    async (action, fields = {}, signal) => {
      try {
        return await apiRequest(
          `crawls/atlas_${action}`,
          {
            crawl_id: crawlId,
            run_id: runId,
            hide_traps: hideTraps,
            ...fields,
          },
          { signal, timeoutMs: 20000 },
        );
      } catch (e) {
        if (!signal?.aborted && e.message === "Server unavailable")
          disconnected.current = true;
        throw e;
      }
    },
    [crawlId, runId, hideTraps],
  );
  const { tiles, error: tileError } = useNetworkTiles(
    call,
    snapshot,
    camera,
    size,
    selection,
    connectionVersion,
  );
  const pickPoint = useCallback(
    async (point) => {
      pickRequest.current?.abort();
      const ctrl = new AbortController();
      pickRequest.current = ctrl;
      try {
        const host = await call(
          "pick",
          { snapshot: snapshot.id, ...point },
          ctrl.signal,
        );
        if (!ctrl.signal.aborted) {
          setSelection(host?.id || null);
          setOffset(0);
        }
      } catch (e) {
        if (!ctrl.signal.aborted) setError(e.message);
      }
    },
    [call, snapshot?.id],
  );
  useEffect(() => () => pickRequest.current?.abort(), [snapshot?.id]);
  useEffect(() => {
    let live = true,
      timer;
    const ctrl = new AbortController();
    async function poll() {
      try {
        const s = await call("status", {}, ctrl.signal);
        if (!live) return;
        setStatus(s);
        setStatusError("");
        if (disconnected.current) {
          disconnected.current = false;
          setError("");
          setConnectionVersion((v) => v + 1);
        }
        const key = `${crawlId}:${runId}:${hideTraps}`;
        if (settingsFor.current !== key) {
          setOptions(s.snapshot?.options || s.defaults || defaults);
          settingsFor.current = key;
        }
        timer = setTimeout(poll, s.job?.state === "running" ? 1000 : 5000);
      } catch (e) {
        if (live) {
          setStatusError(e.message);
          timer = setTimeout(poll, 2000);
        }
      }
    }
    poll();
    return () => {
      live = false;
      clearTimeout(timer);
      ctrl.abort();
    };
  }, [call, busy]);
  useEffect(() => {
    if (!snapshot || snapshot.format !== 2) return;
    const ctrl = new AbortController(),
      timer = setTimeout(() => {
        call(
          "view",
          {
            snapshot: snapshot.id,
            bounds: graphBounds(camera, size.width, size.height),
            node_limit: 50,
            edge_limit: 100,
            counts_only: true,
          },
          ctrl.signal,
        )
          .then((f) => {
            if (ctrl.signal.aborted) return;
            if (
              f.nodes.length > 5000 ||
              f.edges.length + (f.focus?.length || 0) > 20000
            )
              throw Error("Server exceeded render limits");
            setFrame(f);
            setError("");
          })
          .catch((e) => {
            if (!ctrl.signal.aborted) setError(e.message);
          });
      }, 180);
    return () => {
      clearTimeout(timer);
      ctrl.abort();
    };
  }, [call, snapshot?.id, camera, size, connectionVersion]);
  useEffect(() => {
    setInspection(null);
    if (!selection || !snapshot) return;
    const ctrl = new AbortController();
    call(
      "host",
      { snapshot: snapshot.id, host_id: selection, direction, offset },
      ctrl.signal,
    )
      .then((r) => {
        if (!ctrl.signal.aborted) setInspection(r);
      })
      .catch((e) => {
        if (!ctrl.signal.aborted) setError(e.message);
      });
    return () => ctrl.abort();
  }, [call, snapshot?.id, selection, direction, offset, connectionVersion]);
  useEffect(() => {
    if (!snapshot || !query) {
      setMatches([]);
      return;
    }
    const ctrl = new AbortController(),
      timer = setTimeout(
        () =>
          call("search", { query, snapshot: snapshot.id }, ctrl.signal)
            .then((r) => {
              if (!ctrl.signal.aborted) setMatches(r);
            })
            .catch((e) => {
              if (!ctrl.signal.aborted) setError(e.message);
            }),
        250,
      );
    return () => {
      clearTimeout(timer);
      ctrl.abort();
    };
  }, [call, snapshot?.id, query, connectionVersion]);
  async function generate(e) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await call("generate", { options });
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    setSelection(null);
    setInspection(null);
    setOffset(0);
    setFrame(null);
    setCamera({ x: 0.5, y: 0.5, zoom: 1 });
  }, [snapshot?.id]);
  const pick = useCallback((id) => {
    setSelection(id);
    setOffset(0);
  }, []);
  function focus(host) {
    setCamera({ x: host.x, y: host.y, zoom: 30 });
    pick(host.id);
    setMatches([]);
    setQuery("");
  }
  return (
    <main className="crawl-workspace network-page">
      <header className="crawl-heading">
        <h1>Host network</h1>
        <CrawlNavigation crawlId={crawlId} active="Graph" />
      </header>
      {snapshot && (
        <button
          onClick={() =>
            call("status")
              .then(setStatus)
              .catch((e) => setError(e.message))
          }
        >
          Reload snapshot
        </button>
      )}
      <form className="crawl-panel network-generation" onSubmit={generate}>
        <details>
          <summary>Generation parameters</summary>
          <div className="network-controls">
            {[
              ["max_hosts", "Host limit", 10, 2000000],
              ["max_edges", "Edge limit", 10, 10000000],
              ["memory_mb", "Memory budget (MiB)", 64, 4096],
              ["passes", "Analysis passes", 2, 40],
              ["cluster_size", "Cluster size limit", 20, 5000],
            ].map(([key, label, min, max]) => (
              <label key={key}>
                {label}
                <input
                  type="number"
                  min={min}
                  max={max}
                  required
                  value={options[key]}
                  disabled={running}
                  onChange={(e) =>
                    setOptions((o) => ({ ...o, [key]: Number(e.target.value) }))
                  }
                />
              </label>
            ))}
          </div>
          <div
            className="network-filters"
            role="group"
            aria-label="Host filters"
          >
            <label className="crawl-check">
              <input
                type="checkbox"
                checked={options.visited_only}
                disabled={running}
                onChange={(e) =>
                  setOptions((o) => ({ ...o, visited_only: e.target.checked }))
                }
              />
              Visited hosts only
            </label>
            <label className="crawl-check">
              <input
                type="checkbox"
                checked={options.remove_orphans}
                disabled={running}
                onChange={(e) =>
                  setOptions((o) => ({
                    ...o,
                    remove_orphans: e.target.checked,
                  }))
                }
              />
              Exclude orphans
            </label>
            <label className="crawl-check">
              <input
                type="checkbox"
                checked={hideTraps}
                onChange={(event) => {
                  setHideTraps(event.target.checked);
                  setStatus(null);
                  setFrame(null);
                  setError("");
                }}
              />
              Hide trap-excluded hosts
            </label>
          </div>
        </details>
        <button type="submit" disabled={running || busy || !status}>
          {snapshot ? "Regenerate" : "Generate"}
        </button>
        {running && (
          <button
            type="button"
            onClick={() => call("cancel").catch((e) => setError(e.message))}
          >
            Cancel generation
          </button>
        )}
        <span role="status">
          {running
            ? status.job.phase
            : status?.job?.state === "cancelled"
              ? "Generation cancelled"
              : snapshot
                ? `${fmt(snapshot.hosts)} hosts · ${fmt(snapshot.edges)} edges · ${fmt(snapshot.clusters)} clusters`
                : "No snapshot"}
          {snapshot?.hidden_hosts > 0 &&
            ` · ${fmt(snapshot.hidden_hosts)} trap-excluded hosts hidden`}
        </span>
        {status?.job?.error && <p role="alert">{status.job.error}</p>}
      </form>
      {(error || statusError || tileError) && (
        <p role="alert" className="crawl-error">
          {error || statusError || tileError}
        </p>
      )}
      {snapshot && snapshot.format !== 2 && (
        <p>Regenerate the graph to update its layout.</p>
      )}
      {snapshot?.format === 2 && (
        <>
          <p className="network-meta">
            Snapshot: {new Date(snapshot.created).toLocaleString()} · revision{" "}
            {snapshot.revision}
            {snapshot.partial ? " · Partial historical links" : ""}
          </p>
          <div className="network-toolbar">
            <label>
              Find host
              <input
                aria-label="Find host"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </label>
            <button onClick={() => setCamera({ x: 0.5, y: 0.5, zoom: 1 })}>
              Reset view
            </button>
            <button
              aria-label="Zoom in"
              onClick={() =>
                setCamera((c) => ({
                  ...c,
                  zoom: Math.min(10000, c.zoom * 1.5),
                }))
              }
            >
              +
            </button>
            <button
              aria-label="Zoom out"
              onClick={() =>
                setCamera((c) => ({ ...c, zoom: Math.max(0.5, c.zoom / 1.5) }))
              }
            >
              −
            </button>
          </div>
          {matches.length > 0 && (
            <ul className="network-matches">
              {matches.map((h) => (
                <li key={h.id}>
                  <button onClick={() => focus(h)}>{h.name}</button>
                </li>
              ))}
            </ul>
          )}
          <div className="network-layout">
            <section>
              <NetworkCanvas
                frame={frame}
                tiles={tiles}
                onPickPoint={pickPoint}
                totalHosts={snapshot.hosts}
                selectedNode={inspection?.host}
                camera={camera}
                setCamera={setCamera}
                selection={selection}
                onPick={pick}
                onSize={setSize}
              />
              <p role="status">
                {frame ? `${fmt(frame.hosts)} hosts in view` : "Loading graph…"}
              </p>
              <p className="network-legend">
                Color: community · Size: authority (capped) · Blue: incoming ·
                Orange: outgoing
              </p>
            </section>
            <aside className="crawl-panel network-inspector">
              <h2>Host</h2>
              {inspection ? (
                <>
                  <strong className="network-hostname">
                    {inspection.host.name}
                  </strong>
                  <dl>
                    {[
                      ["URLs visited", inspection.host.visited],
                      ["URLs known", inspection.host.known],
                      ["Incoming", inspection.host.incoming],
                      ["Outgoing", inspection.host.outgoing],
                      ["Bridge communities", inspection.host.bridges],
                      ["Community", inspection.host.community],
                      ["Authority", inspection.host.authority.toPrecision(5)],
                    ].map(([label, value]) => (
                      <React.Fragment key={label}>
                        <dt>{label}</dt>
                        <dd>{value}</dd>
                      </React.Fragment>
                    ))}
                  </dl>
                  <button onClick={() => focus(inspection.host)}>
                    Center host
                  </button>
                  <button onClick={() => pick(null)}>Clear selection</button>
                  <label>
                    Connections
                    <select
                      aria-label="Connections"
                      value={direction}
                      onChange={(e) => {
                        setDirection(e.target.value);
                        setOffset(0);
                      }}
                    >
                      <option value="incoming">Incoming</option>
                      <option value="outgoing">Outgoing</option>
                    </select>
                  </label>
                  <ul>
                    {inspection.neighbors.map((h) => (
                      <li key={h.id}>
                        <button onClick={() => focus(h)}>{h.name}</button>
                      </li>
                    ))}
                  </ul>
                  <div>
                    <button
                      disabled={!offset}
                      onClick={() => setOffset((v) => Math.max(0, v - 200))}
                    >
                      Previous
                    </button>
                    <button
                      disabled={!inspection.more}
                      onClick={() => setOffset((v) => v + 200)}
                    >
                      Next
                    </button>
                  </div>
                </>
              ) : (
                <p>{selection ? "Loading host…" : "Select a host"}</p>
              )}
            </aside>
          </div>
        </>
      )}
    </main>
  );
}
