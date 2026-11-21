import { ResultsPanel, RunHistory } from "./runViews";
import { ExecutionLimits } from "./executionLimits";
import { FetchMetrics } from "./fetchMetrics";
import React, { useEffect, useState, useRef } from "react";
import { useHistory, useLocation } from "react-router-dom";
import { uuid4 } from "../../util";
import {
  crawlAPI,
  workspaceAPI,
  clone,
  strategyTitle,
  operationTitle,
  words,
} from "./crawlApi";
import { crawlState, operationActive } from "./crawlStatus";
import { CrawlEditor, Field } from "./crawlEditor";
export { CrawlEditor } from "./crawlEditor";
import "./workspace.css";
export function WorkspaceLanding() {
  const [defaults, setDefaults] = useState(null),
    [failure, setFailure] = useState(""),
    [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    crawlAPI("defaults")
      .then((d) => live && setDefaults(d))
      .catch((e) => {
        if (live) {
          setFailure(e.message);
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
      <p role="status">Loading…</p>
    </main>
  ) : (
    <Workspace defaults={defaults} />
  );
}
export default function Workspace({ defaults }) {
  const location = useLocation(),
    history = useHistory();
  const configuration = location.pathname === "/workspace/configuration";
  const [execution, setExecution] = useState(null);
  const selected = location.pathname.startsWith("/crawls/")
    ? decodeURIComponent(location.pathname.split("/")[2])
    : "";
  const [crawls, setCrawls] = useState([]),
    [editor, setEditor] = useState(null),
    [error, setError] = useState(""),
    [connectionError, setConnectionError] = useState(""),
    [pendingAction, setPendingAction] = useState(""),
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
  const operations = { ...detail?.operations, ...ops };
  const locked = busy || Object.values(operations).some(operationActive);
  const state = crawlState(detail, ops, pendingAction);
  async function refresh() {
    const [list, settings] = await Promise.all([
      crawlAPI("list"),
      workspaceAPI("settings"),
    ]);
    setExecution(settings.execution);
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
    if (configuration) return;
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
  }, [selected, resultsRun, queue, failureOffset, configuration]);
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
  async function perform(fn, action = "") {
    setBusy(true);
    setError("");
    setPendingAction(action);
    try {
      await fn();
      await refresh();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
      setPendingAction("");
    }
  }
  function open(id) {
    if (editor) {
      setError("Unsaved changes");
      return;
    }
    setError("");
    setPendingAction("");
    history.push(id ? `/crawls/${id}` : "/");
  }
  function newCrawl() {
    setError("");
    setPendingAction("");
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
  async function dispatch(action, data) {
    setPendingAction(action);
    const op = await crawlAPI(action, data);
    if (op?.id && op?.state) setOps((old) => ({ ...old, [op.id]: op }));
    return op;
  }
  async function command(action) {
    const op = await crawlAPI("command", {
      crawl_id: selected,
      action,
      id: uuid4(),
    });
    setOps((old) => ({ ...old, [op.id]: op }));
  }
  async function save(start) {
    const e = editor;
    if (!e.record) {
      const signature = JSON.stringify(e.definition);
      if (e.pendingStart && e.pendingStart.signature !== signature) {
        throw Error("Crawl already created; reopen to edit");
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
        await dispatch("start_run", {
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
        throw Error("Invalid pipeline JSON");
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
      await dispatch("activate", {
        crawl_id: c.id,
        revision: c.revision,
        id: pending.activationID,
      });
    setEditor(null);
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
          {(selected || configuration) && (
            <button onClick={() => open("")}>← Crawls</button>
          )}
          {!configuration && (
            <button
              disabled={!!editor || locked}
              onClick={() => history.push("/workspace/configuration")}
            >
              Workspace configuration
            </button>
          )}
          <button
            className="crawl-primary"
            disabled={locked || !!editor || configuration}
            onClick={newCrawl}
          >
            ＋ Create crawl
          </button>
        </div>
      </div>
      {(error || connectionError) && (
        <p role="alert" className="crawl-error">
          {error || connectionError}
        </p>
      )}
      {configuration ? (
        <>
          <h1>Workspace configuration</h1>
          <ExecutionLimits onError={setError} onChange={setExecution} />
        </>
      ) : editor ? (
        <CrawlEditor
          execution={execution}
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
              <button
                onClick={() => history.push(`/crawls/${selected}/report`)}
              >
                Report
              </button>
              <div>
                <span role="status" className={`crawl-state ${state}`}>
                  {words(state)}
                </span>
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
                          dispatch("start_run", {
                            crawl_id: selected,
                            revision: detail.revision,
                          }),
                        )
                      }
                    >
                      {run ? "Restart" : "Start"}
                    </button>
                    {["running", "draining"].includes(state) && (
                      <button
                        disabled={locked}
                        onClick={() => perform(() => command("pause"), "pause")}
                      >
                        Pause
                      </button>
                    )}
                    {state === "stopped" && run?.state !== "completed" && (
                      <button
                        disabled={locked}
                        onClick={() => perform(() => command("start"), "start")}
                      >
                        Resume
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
                              dispatch("continue_run", {
                                crawl_id: selected,
                                revision: detail.revision,
                                id: uuid4(),
                              }),
                            )
                          }
                        >
                          Drain
                        </button>
                      )}
                    {["paused", "pausing"].includes(state) && (
                      <button
                        disabled={locked}
                        onClick={() =>
                          perform(() => command("resume"), "resume")
                        }
                      >
                        Resume
                      </button>
                    )}
                    {[
                      "running",
                      "paused",
                      "draining",
                      "pausing",
                      "starting",
                      "stopping",
                      "failed",
                    ].includes(state) && (
                      <button
                        disabled={locked}
                        onClick={() => perform(() => command("stop"), "stop")}
                      >
                        Stop
                      </button>
                    )}
                    <button
                      disabled={locked}
                      onClick={() => perform(() => edit(detail))}
                    >
                      Edit
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
                  {detail.archived ? "Restore" : "Archive"}
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
                  Delete
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
                      await dispatch("delete", {
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
                  <p>Delete saved data and run history.</p>
                  <label className="crawl-check">
                    <input
                      type="checkbox"
                      checked={preserveLogs}
                      onChange={(e) => setPreserveLogs(e.target.checked)}
                      disabled={busy}
                    />
                    Preserve crawl logs
                  </label>
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
                      Delete
                    </button>
                  </div>
                </form>
              </div>
            )}
            {(detail.draft || detail.expert_draft) && (
              <p className="crawl-notice">
                Draft saved
                <button
                  disabled={locked}
                  onClick={() =>
                    perform(() =>
                      dispatch("activate", {
                        crawl_id: selected,
                        revision: detail.revision,
                      }),
                    )
                  }
                >
                  Apply and restart
                </button>
              </p>
            )}
            <div className="crawl-flow" aria-label="Crawl progress">
              {[
                ["Discovered", stats.admitted],
                ["Deferred discoveries", stats.deferred_discoveries],
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
            {pipelineFailures > 0 && (
              <p role="alert" className="crawl-error">
                Failed pipeline records: {pipelineFailures}
              </p>
            )}
            {run?.continued > 0 && <p>Drain mode</p>}
            {detail.persistence_error && (
              <p role="alert" className="crawl-error">
                Persistence error: {detail.persistence_error}
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
            <h2>Operations</h2>
            {Object.values(ops).length ? (
              <ul>
                {Object.values(ops)
                  .slice(-12)
                  .map((op) => (
                    <li key={op.id} role={op.error ? "alert" : undefined}>
                      {operationTitle(op.action)}: {words(op.state)}
                      {op.error ? ` — ${op.error}` : ""}
                    </li>
                  ))}
              </ul>
            ) : (
              <p>Empty</p>
            )}
            <details>
              <summary>Queues and workers</summary>
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
              <summary>Failed records</summary>
              <Field label="Queue">
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
                Inspect
              </button>
              {!runtime.runtime && <p>Runtime offline</p>}
              {failures?.length === 0 && <p role="status">Empty</p>}
              {!!failures?.length && (
                <nav aria-label="Failed record pages">
                  <p>
                    Records {failureOffset + 1}–
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
                    Previous
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
                    Next
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
                    Retry
                  </button>
                </div>
              ))}
            </details>
          </section>
          <ResultsPanel
            results={results}
            resultsRun={resultsRun}
            setResultsRun={setResultsRun}
          />
          <RunHistory
            runs={detail.runs}
            onView={setResultsRun}
            onReport={(id) =>
              history.push(`/crawls/${selected}/report?run_id=${id}`)
            }
          />
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
              Show archived
            </label>
          </div>
          {!crawls.some((c) => archived || !c.archived) ? (
            <section className="crawl-empty">
              <h2>Empty</h2>
              <button className="crawl-primary" onClick={newCrawl}>
                Create crawl
              </button>
            </section>
          ) : (
            <div className="crawl-library">
              {crawls
                .filter((c) => archived || !c.archived)
                .map((c) => {
                  const r = c.runs.at(-1),
                    status = crawlState(c),
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
                          : `${c.definition.seeds.length} seed URLs · ${c.definition.feeds.length} feeds`}
                      </p>
                      {!c.expert && (
                        <p>
                          {`${strategyTitle(c.definition.strategy)} · ${c.definition.termination.when_drained ? "Drain" : "Monitor"}`}
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
                      <button onClick={() => open(c.id)}>Open →</button>
                    </article>
                  );
                })}
            </div>
          )}
        </>
      ) : (
        <p>Loading…</p>
      )}
    </main>
  );
}
