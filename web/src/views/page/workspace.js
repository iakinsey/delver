import { FetchMetrics } from "./fetchMetrics";
import React, { useEffect, useState, useRef } from "react";
import { useHistory, useLocation } from "react-router-dom";
import { uuid4 } from "../../util";
import {
  crawlAPI,
  clone,
  strategyTitle,
  operationTitle,
  words,
} from "./crawlApi";
import { CrawlEditor, Field } from "./crawlEditor";
export { CrawlEditor } from "./crawlEditor";
import "./workspace.css";
export function WorkspaceLanding({ fallback }) {
  const [defaults, setDefaults] = useState(null),
    [failure, setFailure] = useState(""),
    [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    crawlAPI("defaults")
      .then((d) => live && setDefaults(d))
      .catch((e) => {
        if (live) {
          if (e.legacy) setDefaults(false);
          else setFailure(e.message);
        }
      });
    return () => {
      live = false;
    };
  }, [attempt]);
  if (failure)
    return (
      <main className="crawl-workspace">
        <h1>Workspace unavailable</h1>
        <p role="alert">{failure}</p>
        <button
          onClick={() => {
            setFailure("");
            setAttempt((v) => v + 1);
          }}
        >
          Reconnect
        </button>
      </main>
    );
  return defaults === null ? (
    <main className="crawl-workspace">
      <p role="status">Opening your workspace…</p>
    </main>
  ) : defaults ? (
    <Workspace defaults={defaults} />
  ) : (
    fallback
  );
}
export default function Workspace({ defaults }) {
  const location = useLocation(),
    history = useHistory();
  const selected = location.pathname.startsWith("/crawls/")
    ? decodeURIComponent(location.pathname.split("/")[2])
    : "";
  const [crawls, setCrawls] = useState([]),
    [editor, setEditor] = useState(null),
    [error, setError] = useState(""),
    [connectionError, setConnectionError] = useState(""),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false),
    [archived, setArchived] = useState(false),
    [ops, setOps] = useState({}),
    [queue, setQueue] = useState("requests"),
    [failures, setFailures] = useState(null),
    [failureOffset, setFailureOffset] = useState(0),
    [results, setResults] = useState({ rows: [] }),
    [resultsRun, setResultsRun] = useState("");
  const [deleteOpen, setDeleteOpen] = useState(false),
    [preserveLogs, setPreserveLogs] = useState(true);
  const deleting = useRef("");
  const deleteTrigger = useRef(null);
  function cancelDeletion() {
    setDeleteOpen(false);
    deleteTrigger.current?.focus();
  }
  const selection = useRef("");
  selection.current = `${selected}:${resultsRun}`;
  const detail = crawls.find((c) => c.id === selected),
    run = detail?.runs?.at(-1),
    runtime = detail?.runtime || {},
    frame = runtime.runtime || run?.summary || {},
    stats = frame?.frontier || frame?.queue_states?.frontier || {};
  const pipelineFailures = Object.values(frame.queue_states || {}).reduce(
    (total, counts) => total + (counts.failed || 0),
    0,
  );
  const failureKey = `${selected}:${run?.id || ""}:${queue}`;
  const failurePage = useRef({});
  const failureVersion = useRef(0);
  failurePage.current = {
    key: failureKey,
    loaded: failures !== null,
    offset: failureOffset,
  };
  async function loadFailures(offset, key = failureKey) {
    const version = ++failureVersion.current;
    let rows;
    do {
      rows = await crawlAPI("failed", { crawl_id: selected, queue, offset });
      if (rows.length || offset === 0) break;
      offset = Math.max(0, offset - 50);
    } while (key === failurePage.current.key);
    if (key === failurePage.current.key && version === failureVersion.current) {
      setFailures(rows);
      setFailureOffset(offset);
    }
  }
  const workflowBusy = Object.values(detail?.operations || {}).some((op) =>
    ["accepted", "running"].includes(op.state),
  );
  const locked = busy || workflowBusy;
  const state = workflowBusy
    ? Object.values(detail?.operations || {}).some(
        (op) =>
          op.action === "delete" && ["accepted", "running"].includes(op.state),
      )
      ? "deleting"
      : "starting"
    : run?.state === "completed"
      ? "completed"
      : run?.state === "draining" && runtime.state === "running"
        ? "draining"
        : runtime.state || "not started";
  async function refresh() {
    const list = await crawlAPI("list");
    setCrawls(list);
    const record = list.find((c) => c.id === selected);
    if (record?.deletion) deleting.current = selected;
    if (!record && selected && deleting.current === selected) {
      deleting.current = "";
      setError("");
      history.replace("/");
      return;
    }
    const key = `${selected}:${resultsRun}`;
    if (record && key === selection.current) {
      const o = await crawlAPI("operations", { crawl_id: selected });
      if (key !== selection.current) return;
      setOps(o);
      if (record.runs.length) {
        const data = await crawlAPI("results", {
          crawl_id: selected,
          run_id: resultsRun,
        });
        if (key === selection.current) setResults(data);
      }
      const failed = failurePage.current;
      if (
        key === selection.current &&
        failed.loaded &&
        record.runtime?.runtime
      ) {
        await loadFailures(failed.offset, failed.key);
      }
    }
  }
  useEffect(() => {
    let live = true,
      timer;
    async function poll() {
      try {
        if (document.visibilityState !== "hidden") {
          await refresh();
          if (live) setConnectionError("");
        }
      } catch (e) {
        if (live) setConnectionError(e.message);
      }
      if (live) timer = setTimeout(poll, 2000);
    }
    poll();
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [selected, resultsRun, queue, failureOffset]);
  useEffect(() => {
    setOps({});
    setResults({ rows: [] });
    setFailures(null);
    setFailureOffset(0);
    setResultsRun("");
    setDeleteOpen(false);
  }, [selected]);
  useEffect(() => {
    const guard = (e) => {
      if (editor) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [editor]);
  async function perform(fn) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await fn();
      await refresh();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }
  function open(id) {
    if (editor) {
      setError("Save or discard changes before leaving this editor.");
      return;
    }
    setError("");
    setNotice("");
    history.push(id ? `/crawls/${id}` : "/");
  }
  function newCrawl() {
    setError("");
    setNotice("");
    setEditor({
      record: null,
      definition: clone(defaults),
      pipeline: null,
      createID: uuid4(),
    });
  }
  async function edit(record) {
    let pipeline = null;
    if (record.expert || record.expert_draft) {
      const preview = await crawlAPI("pipeline", { crawl_id: record.id });
      pipeline =
        preview.configuration_json ||
        JSON.stringify(preview.configuration, null, 2);
    }
    setEditor({
      record,
      definition: clone(record.draft || record.definition),
      pipeline,
    });
  }
  async function command(action) {
    await crawlAPI("command", { crawl_id: selected, action, id: uuid4() });
    setNotice("Command accepted. Watch run activity for completion.");
  }
  async function save(start) {
    const e = editor;
    if (!e.record) {
      const signature = JSON.stringify(e.definition);
      if (e.pendingStart && e.pendingStart.signature !== signature) {
        throw Error(
          "This crawl was already saved. Discard this editor and open the saved crawl before changing its configuration.",
        );
      }
      let c = e.pendingStart?.record;
      if (!c) {
        c = await crawlAPI("create", {
          definition: e.definition,
          id: e.createID,
        });
        setEditor(
          (old) => old && { ...old, pendingStart: { record: c, signature } },
        );
      }
      if (start)
        await crawlAPI("start_run", {
          crawl_id: c.id,
          revision: c.revision,
          id: `${e.createID}:start`,
        });
      setEditor(null);
      history.push(`/crawls/${c.id}`);
      return;
    }
    if (e.pipeline !== null) {
      try {
        JSON.parse(e.pipeline);
      } catch {
        throw Error("Pipeline JSON is not valid. Check its syntax.");
      }
    }
    const data =
      e.pipeline === null
        ? { definition: e.definition }
        : {
            configuration_json: e.pipeline,
            definition: {
              name: e.definition.name,
              termination: e.definition.termination,
            },
          };
    const signature = JSON.stringify(data);
    let pending = e.pendingSave;
    if (!pending || pending.signature !== signature) {
      pending = {
        signature,
        request: {
          crawl_id: e.record.id,
          revision: e.record.revision,
          id: uuid4(),
          ...data,
        },
        activationID: uuid4(),
      };
      setEditor((old) => old && { ...old, pendingSave: pending });
    }
    const c = pending.record || (await crawlAPI("draft", pending.request));
    pending = { ...pending, record: c };
    setEditor((old) => old && { ...old, record: c, pendingSave: pending });
    if (start)
      await crawlAPI("activate", {
        crawl_id: c.id,
        revision: c.revision,
        id: pending.activationID,
      });
    setEditor(null);
    setNotice(
      start
        ? "Replacement run started; previous run data is preserved."
        : "Draft saved; the active run is unchanged.",
    );
  }

  return (
    <main className="crawl-workspace">
      <div className="crawl-heading">
        <div>
          {selected && (
            <>
              <h1>{detail?.definition.name || "Crawl"}</h1>
            </>
          )}
        </div>
        <div className="crawl-actions">
          {selected && <button onClick={() => open("")}>← All crawls</button>}
          <button
            className="crawl-primary"
            disabled={locked || !!editor}
            onClick={newCrawl}
          >
            ＋ Create a crawl
          </button>
        </div>
      </div>
      {(error || connectionError) && (
        <p role="alert" className="crawl-error">
          {error || connectionError}
        </p>
      )}
      {notice && (
        <p role="status" className="crawl-notice">
          {notice}
        </p>
      )}
      {editor ? (
        <CrawlEditor
          editor={editor}
          setEditor={setEditor}
          busy={locked}
          onError={setError}
          onCancel={() => {
            setEditor(null);
            setError("");
          }}
          onSave={(start) => perform(() => save(start))}
        />
      ) : selected && detail ? (
        <>
          <section className="crawl-panel">
            <div className="crawl-heading">
              <div>
                <span className={`crawl-state ${state}`}>{words(state)}</span>
                <p>
                  {detail.expert
                    ? "Custom worker pipeline"
                    : `${strategyTitle(detail.definition.strategy)} · ${words(detail.definition.scope)} · ${detail.definition.proxy?.mode && detail.definition.proxy.mode !== "direct" ? detail.definition.proxy.mode.toUpperCase() + " proxy" : "Direct connection"}`}
                </p>
              </div>
              <div className="crawl-actions">
                {!detail.archived && (
                  <>
                    <button
                      disabled={locked}
                      onClick={() =>
                        perform(() =>
                          crawlAPI("start_run", {
                            crawl_id: selected,
                            revision: detail.revision,
                          }),
                        )
                      }
                    >
                      {run ? "Start a new run" : "Start crawl"}
                    </button>
                    {["running", "draining"].includes(state) && (
                      <button
                        disabled={locked}
                        onClick={() => perform(() => command("pause"))}
                      >
                        Pause
                      </button>
                    )}
                    {state === "stopped" && run?.state !== "completed" && (
                      <button
                        disabled={locked}
                        onClick={() => perform(() => command("start"))}
                      >
                        Resume this run
                      </button>
                    )}
                    {["stopped", "completed", "failed"].includes(state) &&
                      (stats.pending_and_active || 0) +
                        (stats.deferred_discoveries || 0) >
                        0 && (
                        <button
                          disabled={locked}
                          onClick={() =>
                            perform(() =>
                              crawlAPI("continue_run", {
                                crawl_id: selected,
                                revision: detail.revision,
                                id: uuid4(),
                              }),
                            )
                          }
                        >
                          Continue remaining work until drained
                        </button>
                      )}
                    {state === "paused" && (
                      <button
                        disabled={locked}
                        onClick={() => perform(() => command("resume"))}
                      >
                        Resume
                      </button>
                    )}
                    {["running", "paused", "draining"].includes(state) && (
                      <button
                        disabled={locked}
                        onClick={() => perform(() => command("stop"))}
                      >
                        Stop
                      </button>
                    )}
                    <button
                      disabled={locked}
                      onClick={() => perform(() => edit(detail))}
                    >
                      Edit crawl
                    </button>
                  </>
                )}
                <button
                  disabled={locked}
                  onClick={() =>
                    perform(async () => {
                      const c = await crawlAPI("duplicate", {
                        crawl_id: selected,
                      });
                      history.push(`/crawls/${c.id}`);
                    })
                  }
                >
                  Duplicate
                </button>
                <button
                  disabled={locked}
                  onClick={() =>
                    perform(() =>
                      crawlAPI(detail.archived ? "restore" : "archive", {
                        crawl_id: selected,
                      }),
                    )
                  }
                >
                  {detail.archived ? "Restore crawl" : "Archive crawl"}
                </button>
                <button
                  ref={deleteTrigger}
                  disabled={locked}
                  onClick={() => {
                    setPreserveLogs(true);
                    setError("");
                    setDeleteOpen(true);
                  }}
                >
                  Delete crawl
                </button>
              </div>
            </div>
            {deleteOpen && (
              <div className="crawl-delete-backdrop">
                <form
                  role="alertdialog"
                  aria-modal="true"
                  aria-labelledby="delete-crawl-title"
                  className="crawl-delete-dialog"
                  onKeyDown={(e) => {
                    if (e.key === "Escape" && !busy) cancelDeletion();
                    if (e.key === "Tab") {
                      const controls = [
                        ...e.currentTarget.querySelectorAll("input,button"),
                      ].filter((el) => !el.disabled);
                      const first = controls[0],
                        last = controls.at(-1);
                      if (e.shiftKey && document.activeElement === first) {
                        e.preventDefault();
                        last?.focus();
                      }
                      if (!e.shiftKey && document.activeElement === last) {
                        e.preventDefault();
                        first?.focus();
                      }
                    }
                  }}
                  onSubmit={(e) => {
                    e.preventDefault();
                    perform(async () => {
                      deleting.current = selected;
                      await crawlAPI("delete", {
                        crawl_id: selected,
                        revision: detail.revision,
                        id: uuid4(),
                        confirm: true,
                        preserve_logs: preserveLogs,
                      });
                      deleting.current = selected;
                      setDeleteOpen(false);
                    });
                  }}
                >
                  {error && <p role="alert">{error}</p>}
                  <h2 id="delete-crawl-title">
                    Delete {detail.definition.name}?
                  </h2>
                  <p>
                    This permanently removes its saved data and all run history.
                  </p>
                  <label className="crawl-check">
                    <input
                      type="checkbox"
                      checked={preserveLogs}
                      onChange={(e) => setPreserveLogs(e.target.checked)}
                      disabled={busy}
                    />
                    Preserve crawl logs
                  </label>
                  <p>
                    <small>
                      Checked keeps Elasticsearch logs. Unchecked deletes this
                      crawl’s Elasticsearch logs.
                    </small>
                  </p>
                  <div className="crawl-actions">
                    <button
                      autoFocus
                      type="button"
                      disabled={busy}
                      onClick={cancelDeletion}
                    >
                      Cancel
                    </button>
                    <button type="submit" disabled={busy}>
                      Delete permanently
                    </button>
                  </div>
                </form>
              </div>
            )}
            {(detail.draft || detail.expert_draft) && (
              <p className="crawl-notice">
                A draft is saved. Applying it starts a replacement run.
                <button
                  disabled={locked}
                  onClick={() =>
                    perform(() =>
                      crawlAPI("activate", {
                        crawl_id: selected,
                        revision: detail.revision,
                      }),
                    )
                  }
                >
                  Apply draft and start new run
                </button>
              </p>
            )}
            <div className="crawl-flow" aria-label="Crawl progress">
              {[
                ["Discovered", stats.admitted],
                ["Waiting for room", stats.deferred_discoveries],
                ["Fetch queue", frame?.queues?.requests],
                ["Frontier backlog", stats.pending_and_active],
                ["Processed", stats.terminal_outcomes],
                ["Unique HTML", stats.unique_html],
                ["Failed fetches", stats.failed_fetches],
              ].map(([label, value]) => (
                <div key={label}>
                  <strong>{value || 0}</strong>
                  <span>{label}</span>
                </div>
              ))}
            </div>
            <p role="status">
              {state === "completed"
                ? "This run is complete. Results and history are preserved."
                : state === "draining"
                  ? "A termination condition was reached. Dispatched work is finishing; remaining discoveries are preserved."
                  : state === "paused"
                    ? "Paused. Queues and discoveries are preserved."
                    : state === "stopped"
                      ? "Stopped. Results and pending work are preserved; resume this run or start a new one."
                      : pipelineFailures > 0
                        ? "Pipeline processing needs attention. Failed records are preserved for retry."
                        : state === "running" &&
                            !stats.pending_and_active &&
                            !stats.deferred_discoveries
                          ? run?.continued > 0 ||
                            detail.definition.termination.when_drained
                            ? "Waiting for sources and processing to settle before completing."
                            : "Caught up. Monitoring continues."
                          : "Discoveries enter the queue as capacity becomes available."}
            </p>
            {pipelineFailures > 0 && (
              <p role="alert" className="crawl-error">
                {pipelineFailures} pipeline records failed. Open Recover failed
                work to inspect and retry them.
              </p>
            )}
            {state === "completed" &&
              (stats.pending_and_active || 0) +
                (stats.deferred_discoveries || 0) >
                0 && (
                <p>
                  {(stats.pending_and_active || 0) +
                    (stats.deferred_discoveries || 0)}{" "}
                  discoveries remain preserved outside the drained fetch queue
                  because this run reached a termination limit.
                </p>
              )}
            {!runtime.runtime && run?.summary && (
              <p>
                <small>
                  Saved queue snapshot from the last running session. Remaining
                  discoveries can be continued without restarting from the
                  original URLs.
                </small>
              </p>
            )}
            {run?.continued > 0 && (
              <p className="crawl-notice">
                {state === "completed"
                  ? "Preserved work was continued until drained."
                  : "Continuing preserved work until drained."}{" "}
                The previous request and time limits no longer apply; source
                polling and scheduled revisits are disabled.
              </p>
            )}
            {detail.persistence_error && (
              <p role="alert" className="crawl-error">
                Workspace changes could not be saved: {detail.persistence_error}
              </p>
            )}
            {runtime.error && <p className="crawl-error">{runtime.error}</p>}
            {Object.entries(frame?.health || {}).map(([name, text]) => (
              <p role="alert" key={name}>
                {name}: {text}
              </p>
            ))}
          </section>
          <FetchMetrics data={frame.fetch} frontier={stats} />
          <section className="crawl-panel">
            <h2>Run activity</h2>
            {Object.values(ops).length ? (
              <ul>
                {Object.values(ops)
                  .slice(-12)
                  .map((op) => (
                    <li key={op.id}>
                      {operationTitle(op.action)}: {words(op.state)}
                      {op.error ? ` — ${op.error}` : ""}
                    </li>
                  ))}
              </ul>
            ) : (
              <p>No commands yet.</p>
            )}
            <details>
              <summary>Queue and worker details</summary>
              <div className="crawl-table-scroll">
                <table>
                  <thead>
                    <tr>
                      <th>Queue</th>
                      <th>Pending</th>
                      <th>In progress</th>
                      <th>Failed</th>
                    </tr>
                  </thead>
                  <tbody>
                    {Object.entries(frame?.queue_states || {})
                      .filter(([n]) => n !== "frontier")
                      .map(([n, c]) => (
                        <tr key={n}>
                          <td>{n}</td>
                          <td>{c.pending || 0}</td>
                          <td>{c.leased || 0}</td>
                          <td>{c.failed || 0}</td>
                        </tr>
                      ))}
                  </tbody>
                </table>
                <table>
                  <thead>
                    <tr>
                      <th>Worker</th>
                      <th>Active</th>
                      <th>Processed</th>
                      <th>Errors</th>
                    </tr>
                  </thead>
                  <tbody>
                    {Object.entries(frame?.workers || {}).map(([n, v]) => (
                      <tr key={n}>
                        <td>{n}</td>
                        <td>{v.active || 0}</td>
                        <td>{v.processed || 0}</td>
                        <td>
                          {v.errors || 0}
                          {v.last_error && (
                            <details>
                              <summary>Last error</summary>
                              <p>{v.last_error}</p>
                            </details>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </details>
            <details>
              <summary>Recover failed work</summary>
              <Field label="Queue name">
                <input
                  value={queue}
                  onChange={(e) => {
                    setQueue(e.target.value);
                    setFailures(null);
                    setFailureOffset(0);
                  }}
                />
              </Field>
              <button
                disabled={locked || !queue || !runtime.runtime}
                onClick={() => perform(() => loadFailures(0))}
              >
                Inspect failed records
              </button>
              {!runtime.runtime && (
                <p>Resume this run to inspect or retry failed queue records.</p>
              )}
              {failures?.length === 0 && (
                <p role="status">No failed records in this queue.</p>
              )}
              {!!failures?.length && (
                <nav aria-label="Failed record pages">
                  <p>
                    Showing failed records {failureOffset + 1}–
                    {failureOffset + failures.length}.
                  </p>
                  <button
                    disabled={locked || !runtime.runtime || failureOffset === 0}
                    onClick={() =>
                      perform(() =>
                        loadFailures(Math.max(0, failureOffset - 50)),
                      )
                    }
                  >
                    Previous failed records
                  </button>
                  <button
                    disabled={
                      locked ||
                      !runtime.runtime ||
                      failures.length < 50 ||
                      (frame.queue_states?.[queue]?.failed !== undefined &&
                        failureOffset + failures.length >=
                          frame.queue_states[queue].failed)
                    }
                    onClick={() =>
                      perform(() => loadFailures(failureOffset + 50))
                    }
                  >
                    Next failed records
                  </button>
                </nav>
              )}
              {(failures || []).map((f) => (
                <div key={f.id}>
                  <p>{f.error || f.id}</p>
                  <button
                    disabled={locked}
                    onClick={() =>
                      perform(() =>
                        crawlAPI("command", {
                          crawl_id: selected,
                          action: "replay",
                          id: uuid4(),
                          queue,
                          record: f.id,
                        }),
                      )
                    }
                  >
                    Retry record
                  </button>
                </div>
              ))}
            </details>
          </section>
          <section className="crawl-panel">
            <h2>Recent results</h2>
            {resultsRun && (
              <p>
                Showing a previous run.{" "}
                <button onClick={() => setResultsRun("")}>
                  View latest run
                </button>
              </p>
            )}
            {results.available === false ? (
              <p>
                This pipeline uses a custom output sink. Results remain in that
                sink.
              </p>
            ) : results.rows?.length ? (
              <div className="crawl-table-scroll">
                <table>
                  <thead>
                    <tr>
                      <th>Page</th>
                      <th>HTTP status</th>
                      <th>Title or error</th>
                    </tr>
                  </thead>
                  <tbody>
                    {results.rows.map((row, i) => (
                      <tr key={`${row.uri}:${i}`}>
                        <td className="crawl-result-uri">
                          {/^https?:\/\//.test(row.uri) ? (
                            <a href={row.uri} target="_blank" rel="noreferrer">
                              {row.uri}
                            </a>
                          ) : (
                            row.uri
                          )}
                        </td>
                        <td>{row.http_status || "Unavailable"}</td>
                        <td>{row.error || row.title || "Untitled page"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <p>No page results yet.</p>
            )}
            {results.limited && (
              <small>
                This preview reads a bounded tail. Full records remain in the
                run's output storage.
              </small>
            )}
          </section>
          <section className="crawl-panel">
            <h2>Run history</h2>
            <ul>
              {[...detail.runs].reverse().map((r) => (
                <li key={r.id}>
                  {new Date(r.created * 1000).toLocaleString()} · version{" "}
                  {r.version} · {words(r.state)}
                  {r.continued > 0
                    ? ` · continued ${r.continued} time(s)`
                    : ""}{" "}
                  <button onClick={() => setResultsRun(r.id)}>
                    View results for this run
                  </button>
                  <details>
                    <summary>Configuration for this run</summary>
                    <p>
                      {r.expert
                        ? "Custom pipeline preserved in expert configuration."
                        : `${words(r.definition.strategy)}; ${words(r.definition.scope)}; ${r.definition.termination.when_drained ? "finish when drained" : "monitor every " + r.definition.revisit}`}
                    </p>
                  </details>
                </li>
              ))}
            </ul>
          </section>
        </>
      ) : !selected ? (
        <>
          <div className="crawl-library-toolbar">
            <p>{crawls.filter((c) => !c.archived).length} crawl definitions</p>
            <label className="crawl-check">
              <input
                type="checkbox"
                checked={archived}
                onChange={(e) => setArchived(e.target.checked)}
              />
              Show archived crawls
            </label>
          </div>
          {!crawls.some((c) => archived || !c.archived) ? (
            <section className="crawl-empty">
              <div className="crawl-orbit" aria-hidden="true">
                ◎ → ◉ → ◎
              </div>
              <h2>Where should we start?</h2>
              <p>
                Add a website or a few starting pages. Explore a site, follow
                the wider web, or keep watching for changes.
              </p>
              <button className="crawl-primary" onClick={newCrawl}>
                Create your first crawl
              </button>
            </section>
          ) : (
            <div className="crawl-library">
              {crawls
                .filter((c) => archived || !c.archived)
                .map((c) => {
                  const r = c.runs.at(-1),
                    status =
                      r?.state === "completed"
                        ? "completed"
                        : c.runtime?.state || "not started",
                    snapshot = c.runtime?.runtime || r?.summary || {},
                    f =
                      snapshot.frontier ||
                      snapshot.queue_states?.frontier ||
                      {};
                  return (
                    <article className="crawl-panel crawl-card" key={c.id}>
                      <span className="crawl-state">
                        {c.archived ? "archived" : words(status)}
                      </span>
                      <h2>
                        <button
                          className="crawl-title-button"
                          onClick={() => open(c.id)}
                        >
                          {c.definition.name}
                        </button>
                      </h2>
                      <p>
                        {c.expert
                          ? "Custom pipeline"
                          : `${c.definition.seeds.length} starting URLs · ${c.definition.feeds.length} feeds`}
                      </p>
                      {!c.expert && (
                        <p>
                          {`${strategyTitle(c.definition.strategy)} · ${c.definition.termination.when_drained ? "Finish when drained" : "Continuous monitoring"}`}
                        </p>
                      )}
                      <div className="crawl-card-metrics">
                        <span>
                          <strong>{f.terminal_outcomes || 0}</strong> processed
                        </span>
                        <span>
                          <strong>
                            {(f.pending_and_active || 0) +
                              (f.deferred_discoveries || 0)}
                          </strong>{" "}
                          waiting
                        </span>
                      </div>
                      <button onClick={() => open(c.id)}>Open crawl →</button>
                    </article>
                  );
                })}
            </div>
          )}
        </>
      ) : (
        <p>Loading crawl…</p>
      )}
    </main>
  );
}
