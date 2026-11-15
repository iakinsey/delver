import React, { useEffect, useState, useRef } from "react";
import { request, uuid4 } from "../../util";
import { btn as baseButton, input as baseInput } from "../../styles/input";
const btn = {
  ...baseButton,
  width: "auto",
  fontSize: "1em",
  padding: ".5em .8em",
  margin: ".3em",
};
const input = {
  ...baseInput,
  width: "100%",
  fontSize: "1em",
  margin: ".3em 0",
  boxSizing: "border-box",
};
import { BOX_BG, LINK_TEXT_ALT } from "../../styles/color";
export default function Crawler() {
  const [status, setStatus] = useState({}),
    [ops, setOperations] = useState({}),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [tab, setTab] = useState("Overview");
  const [text, setConfiguration] = useState(""),
    [dirty, setDirty] = useState(false),
    [pending, setPending] = useState(false),
    [queue, setQueue] = useState(""),
    [records, setRecords] = useState([]),
    [offset, setOffset] = useState(0),
    [record, setRecord] = useState("");
  const [interval, setInterval] = useState("5m"),
    [preview, setPreview] = useState(null),
    [sourceType, setSourceType] = useState("fixed_seed_publisher"),
    [uris, setUris] = useState("");
  const retry = useRef(null);
  async function call(action, data = {}) {
    const r = await request(`crawler/${action}`, data);
    if (!r.success) throw Error(r.error || "Request failed");
    return r.data;
  }
  async function refresh() {
    const [s, o] = await Promise.all([call("status"), call("operations")]);
    setStatus(s);
    setOperations(o);
  }
  useEffect(() => {
    let live = true,
      timer,
      failures = 0;
    async function poll() {
      if (document.visibilityState !== "hidden") {
        try {
          await refresh();
          failures = 0;
        } catch (e) {
          if (live) setError(e.message);
          failures++;
        }
      }
      if (live) timer = setTimeout(poll, Math.min(30000, 2000 * 2 ** failures));
    }
    poll();
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, []);
  useEffect(() => {
    const guard = (e) => {
      if (dirty) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [dirty]);
  async function perform(fn) {
    setPending(true);
    setError("");
    setNotice("");
    try {
      await fn();
    } catch (e) {
      setError(e.message);
    } finally {
      setPending(false);
    }
  }
  async function command(action, id = record) {
    const previous = retry.current;
    const payload =
      previous?.action === action &&
      previous?.record === id &&
      previous?.queue === queue
        ? previous
        : { id: uuid4(), action, queue, record: id };
    retry.current = payload;
    const op = await call("command", payload);
    retry.current = null;
    setOperations((old) => ({ ...old, [op.id]: op }));
    setNotice(`Command ${op.state}; completion appears below.`);
    await refresh();
  }
  async function inspect(page) {
    setRecords(await call("failed", { queue, offset: page }));
    setOffset(page);
  }
  function addSource() {
    const app = JSON.parse(text),
      urls = uris.split(/\s+/).filter(Boolean);
    if (!urls.length) throw Error("Enter source URLs");
    urls.forEach((uri) => {
      if (!["http:", "https:"].includes(new URL(uri).protocol))
        throw Error("Use HTTP or HTTPS");
    });
    const fetcher = app.workers.find((w) => w.type === "http_fetcher");
    if (!fetcher) throw Error("Configure a fetcher first");
    app.workers.push({
      name: `source_${uuid4()}`,
      type: sourceType,
      manager: "job",
      interval,
      outbox: fetcher.inbox,
      parameters: { uris: urls },
    });
    setConfiguration(JSON.stringify(app, null, 2));
    setDirty(true);
    setUris("");
  }
  const busy =
    pending ||
    Object.values(ops).some((o) => ["accepted", "running"].includes(o.state));
  return (
    <main
      style={{
        fontFamily: "monospace",
        maxWidth: 1100,
        margin: "30px auto",
        padding: 16,
      }}
    >
      <h1 style={{ color: LINK_TEXT_ALT }}>Crawler</h1>
      <nav aria-label="Crawler views">
        {["Overview", "Configuration", "Recovery"].map((name) => (
          <button
            style={btn}
            key={name}
            aria-pressed={tab === name}
            onClick={() => {
              setTab(name);
              if (name === "Configuration" && !text)
                perform(async () => {
                  const d = await call("draft");
                  setConfiguration(JSON.stringify(d.configuration, null, 2));
                });
            }}
          >
            {name}
          </button>
        ))}
      </nav>
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      {tab === "Overview" && (
        <section style={panel}>
          <h2>Runtime: {status.state || "Connecting…"}</h2>
          <p>
            Desired: {status.desired} · Version: {status.version}
          </p>
          {status.error && <p role="alert">{status.error}</p>}
          {["start", "pause", "resume", "stop"].map((action) => (
            <button
              key={action}
              style={btn}
              disabled={
                busy ||
                !{
                  start: ["stopped"],
                  pause: ["running"],
                  resume: ["paused", "pausing"],
                  stop: ["running", "paused", "pausing", "failed"],
                }[action].includes(status.state)
              }
              onClick={() => perform(() => command(action))}
            >
              {action}
            </button>
          ))}
          {Object.entries(status.runtime?.health || {}).map(([name, error]) => (
            <p role="alert" key={name}>
              {name}: {error}
            </p>
          ))}
          <h3>Queues</h3>
          <ul>
            {Object.entries(status.runtime?.queues || {}).map(([name, n]) => (
              <li key={name}>
                {name}: {n < 0 ? "Unavailable" : n}{" "}
                {status.runtime?.queue_states?.[name] &&
                  Object.entries(status.runtime.queue_states[name])
                    .map(([state, count]) => `${state}: ${count}`)
                    .join(" · ")}
              </li>
            ))}
          </ul>
          <h3>Sources</h3>
          <ul>
            {(status.sources || []).map((source) => (
              <li key={source.name}>
                {source.name}: {source.type}, every {source.interval}
                <ul>
                  {(source.uris || []).map((uri) => (
                    <li key={uri} style={{ overflowWrap: "anywhere" }}>
                      {uri}
                    </li>
                  ))}
                </ul>
              </li>
            ))}
          </ul>
          <h3>Workers</h3>
          <div style={{ overflowX: "auto" }}>
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Active</th>
                  <th>Processed</th>
                  <th>Errors</th>
                  <th>Last success</th>
                  <th>Last error</th>
                </tr>
              </thead>
              <tbody>
                {Object.entries(status.runtime?.workers || {}).map(
                  ([name, w]) => (
                    <tr key={name}>
                      <td>{name}</td>
                      <td>{w.active}</td>
                      <td>{w.processed}</td>
                      <td>{w.errors}</td>
                      <td>
                        {w.last_success
                          ? new Date(w.last_success * 1000).toLocaleString()
                          : "None"}
                      </td>
                      <td>{w.last_error || "None"}</td>
                    </tr>
                  ),
                )}
              </tbody>
            </table>
          </div>
        </section>
      )}
      {tab === "Configuration" && (
        <section style={panel}>
          <h2>Configuration draft</h2>
          <p>
            Save and validate before activation. Activation gracefully restarts
            the crawler.
          </p>
          <label>
            Source type{" "}
            <select
              style={input}
              value={sourceType}
              onChange={(e) => setSourceType(e.target.value)}
            >
              <option value="fixed_seed_publisher">Fixed URLs</option>
              <option value="rss_feed_publisher">RSS / Atom</option>
            </select>
          </label>
          <label style={{ display: "block" }}>
            Source interval{" "}
            <input
              style={input}
              value={interval}
              onChange={(e) => setInterval(e.target.value)}
            />
          </label>
          <label style={{ display: "block" }}>
            Source URLs
            <textarea
              style={{ ...input, width: "100%" }}
              value={uris}
              onChange={(e) => setUris(e.target.value)}
            />
          </label>
          <button
            style={btn}
            disabled={pending || !text}
            onClick={() => perform(async () => addSource())}
          >
            Add source to draft
          </button>
          <label style={{ display: "block" }}>
            Pipeline JSON
            <textarea
              spellCheck="false"
              style={{
                ...input,
                width: "100%",
                minHeight: 360,
                boxSizing: "border-box",
              }}
              value={text}
              onChange={(e) => {
                setConfiguration(e.target.value);
                setDirty(true);
              }}
            />
          </label>
          <button
            style={btn}
            disabled={pending}
            onClick={() =>
              perform(async () => {
                const result = await call("validate", {
                  configuration: JSON.parse(text),
                });
                setPreview(result.resolved.configuration);
                setNotice("Configuration valid");
              })
            }
          >
            Validate
          </button>
          <button
            style={btn}
            disabled={pending}
            onClick={() =>
              perform(async () => {
                await call("draft", { configuration: JSON.parse(text) });
                setDirty(false);
                setNotice("Draft saved");
              })
            }
          >
            Save draft
          </button>
          <button
            style={btn}
            disabled={busy || dirty}
            onClick={() => perform(() => command("activate"))}
          >
            Activate saved draft
          </button>
          <button
            style={btn}
            disabled={busy || dirty}
            onClick={() => perform(() => command("rollback"))}
          >
            Roll back
          </button>
          {dirty && <p role="status">Unsaved changes</p>}
          {preview && (
            <details>
              <summary>Resolved configuration preview</summary>
              <pre style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
                {JSON.stringify(preview, null, 2)}
              </pre>
            </details>
          )}
        </section>
      )}
      {tab === "Recovery" && (
        <section style={panel}>
          <h2>Failed work</h2>
          <label>
            Queue name{" "}
            <input
              style={input}
              value={queue}
              onChange={(e) => {
                setQueue(e.target.value);
                setRecords([]);
              }}
            />
          </label>
          <button
            style={btn}
            disabled={pending || !queue}
            onClick={() => perform(() => inspect(0))}
          >
            Inspect
          </button>
          <ul>
            {records.map((r) => (
              <li key={r.id}>
                <code style={{ overflowWrap: "anywhere" }}>{r.id}</code> ·
                attempts {r.attempts} {r.error && `— ${r.error}`}{" "}
                <button
                  style={btn}
                  disabled={busy}
                  onClick={() =>
                    perform(async () => {
                      await command("replay", r.id);
                      await inspect(offset);
                    })
                  }
                >
                  Replay
                </button>
              </li>
            ))}
          </ul>
          <button
            style={btn}
            disabled={pending || !offset}
            onClick={() => perform(() => inspect(Math.max(0, offset - 50)))}
          >
            Previous
          </button>
          <button
            style={btn}
            disabled={pending || records.length < 50}
            onClick={() => perform(() => inspect(offset + 50))}
          >
            Next
          </button>
          <h3>Recrawl a completed URL</h3>
          <label>
            Normalized URL{" "}
            <input
              style={{ ...input, width: "100%", boxSizing: "border-box" }}
              value={record}
              onChange={(e) => setRecord(e.target.value)}
            />
          </label>
          <button
            style={btn}
            disabled={busy || !queue || !record}
            onClick={() => perform(() => command("recrawl"))}
          >
            Schedule recrawl
          </button>
        </section>
      )}
      <section style={panel}>
        <h2>Operations</h2>
        <ul>
          {Object.values(ops)
            .sort((a, b) => b.updated - a.updated)
            .map((o) => (
              <li key={o.id}>
                {o.action}: {o.state}
                {o.error && ` — ${o.error}`}
              </li>
            ))}
        </ul>
      </section>
    </main>
  );
}
const panel = {
  background: BOX_BG,
  border: "1px dashed #6c6c6c",
  padding: 16,
  marginTop: 18,
};
