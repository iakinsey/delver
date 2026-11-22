import React, { useEffect, useState } from "react";
import { workspaceAPI } from "./crawlApi";
import { Field } from "./crawlEditor";
const preset =
  "https://cdn.jsdelivr.net/gh/hagezi/dns-blocklists@latest/wildcard/tif.mini-onlydomains.txt";
export function ProtectionSettings({ section, onError }) {
  const [state, setState] = useState(null),
    [editor, setEditor] = useState(null),
    [preview, setPreview] = useState(null),
    [busy, setBusy] = useState(false),
    [search, setSearch] = useState(""),
    [query, setQuery] = useState(""),
    [matches, setMatches] = useState(null),
    [incident, setIncident] = useState(null),
    [incidentOffset, setIncidentOffset] = useState(0),
    [incidentSearch, setIncidentSearch] = useState("");
  const call = (action, data = {}) =>
    workspaceAPI("protection", { action, ...data });
  async function perform(fn) {
    setBusy(true);
    onError("");
    try {
      await fn();
    } catch (e) {
      onError(e.message);
    } finally {
      setBusy(false);
    }
  }
  const load = async () => {
    const next = await call("state");
    setState(next);
    if (incident)
      setIncident(
        await call("incident", { id: incident.id, offset: incidentOffset }),
      );
  };
  useEffect(() => {
    perform(load);
  }, []);
  const edit = (key, value) => {
    setEditor((e) => ({ ...e, [key]: value }));
    setPreview(null);
  };
  async function open(id) {
    const list = await call("list", { id });
    setEditor({ ...list, url: list.url || "", text: list.text || "" });
    setPreview(null);
  }
  async function inspect(list) {
    const result = await call("preview", { revision: state.revision, list });
    setEditor({ ...list, url: list.url || "", text: list.text || "" });
    setPreview(result);
  }
  useEffect(() => {
    let live = true,
      timer;
    async function poll() {
      try {
        const latest = await call("state");
        const rows = incidentSearch
          ? await call("incidents", { query: incidentSearch })
          : latest.incidents;
        if (live)
          setState((s) =>
            s ? { ...s, incidents: rows, health: latest.health } : s,
          );
        if (incident) {
          const updated = await call("incident", {
            id: incident.id,
            offset: incidentOffset,
          });
          if (live) setIncident(updated);
        }
      } catch (e) {
        if (live) onError(e.message);
      }
      if (live) timer = setTimeout(poll, 2000);
    }
    timer = setTimeout(poll, 2000);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [incident?.id, incidentOffset, incidentSearch]);
  if (!state) return <p role="status">Loading protection configuration…</p>;
  return (
    <section className="crawl-panel">
      <fieldset disabled={busy} className="protection-controls">
        <h2>
          {section === "blocklists" ? "Blocklists" : "Restricted content"}
        </h2>
        <button disabled={busy} onClick={() => perform(load)}>
          Reload
        </button>
        {section === "blocklists" ? (
          <>
            <Field label="Search lists">
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
            </Field>
            <button
              disabled={busy}
              onClick={() => {
                setEditor({
                  id: "",
                  name: "",
                  enabled: true,
                  text: "",
                  url: "",
                  refresh: "6h",
                });
                setPreview(null);
              }}
            >
              Add list
            </button>
            <div className="crawl-table-scroll">
              <table>
                <thead>
                  <tr>
                    {[
                      "Name",
                      "Enabled",
                      "Source",
                      "Entries",
                      "Updated",
                      "Status",
                      "Actions",
                    ].map((v) => (
                      <th key={v}>{v}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {state.lists
                    .filter((l) =>
                      `${l.name} ${l.url}`
                        .toLowerCase()
                        .includes(search.toLowerCase()),
                    )
                    .map((l) => (
                      <tr key={l.id}>
                        <td>{l.name}</td>
                        <td>{l.enabled ? "Enabled" : "Disabled"}</td>
                        <td>{l.url || "Pasted"}</td>
                        <td>{l.entries}</td>
                        <td>
                          {l.updated && !l.updated.startsWith("0001")
                            ? `${new Date(l.updated).toLocaleString()} (${Math.max(0, Math.floor((Date.now() - Date.parse(l.updated)) / 60000))}m old)`
                            : "Never"}
                        </td>
                        <td>
                          {!l.updated || l.updated.startsWith("0001")
                            ? "Inactive"
                            : "Loaded"}
                          {l.error && <p role="status">{l.error}</p>}
                        </td>
                        <td>
                          <button
                            disabled={busy}
                            onClick={() => perform(() => open(l.id))}
                          >
                            Edit {l.name}
                          </button>
                          <button
                            disabled={busy}
                            onClick={() =>
                              perform(async () => {
                                if (l.enabled) {
                                  setState(
                                    await call("disable", {
                                      id: l.id,
                                      revision: state.revision,
                                    }),
                                  );
                                } else {
                                  const full = await call("list", { id: l.id });
                                  await inspect({ ...full, enabled: true });
                                }
                              })
                            }
                          >
                            {l.enabled ? "Disable" : "Enable"} {l.name}
                          </button>
                          <button
                            disabled={busy}
                            onClick={() =>
                              perform(async () =>
                                inspect(await call("list", { id: l.id })),
                              )
                            }
                          >
                            Refresh {l.name}
                          </button>
                          <button
                            disabled={busy}
                            onClick={() =>
                              perform(async () =>
                                setState(
                                  await call("delete", {
                                    id: l.id,
                                    revision: state.revision,
                                  }),
                                ),
                              )
                            }
                          >
                            Delete {l.name}
                          </button>
                        </td>
                      </tr>
                    ))}
                </tbody>
              </table>
            </div>
            {editor && (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  perform(() => inspect(editor));
                }}
              >
                <h3>{editor.id ? "Edit list" : "Add list"}</h3>
                <Field label="List name">
                  <input
                    required
                    value={editor.name}
                    onChange={(e) => edit("name", e.target.value)}
                  />
                </Field>
                <fieldset>
                  <legend>Source</legend>
                  {["Pasted entries", "Feed URL"].map((v, i) => (
                    <label key={v} className="crawl-check">
                      <input
                        type="radio"
                        name="list-source"
                        checked={i ? editor.url !== "" : editor.url === ""}
                        onChange={() => {
                          setEditor((e) => ({
                            ...e,
                            url: i ? "https://" : "",
                            text: i ? "" : e.text,
                          }));
                          setPreview(null);
                        }}
                      />
                      {v}
                    </label>
                  ))}
                </fieldset>
                {editor.url !== "" ? (
                  <>
                    <Field label="Feed URL">
                      <input
                        type="url"
                        required
                        value={editor.url}
                        onChange={(e) => edit("url", e.target.value)}
                      />
                    </Field>
                    <button
                      type="button"
                      onClick={() => {
                        setEditor({
                          ...editor,
                          name: editor.name || "HaGeZi threats",
                          url: preset,
                        });
                        setPreview(null);
                      }}
                    >
                      HaGeZi threat feed
                    </button>
                  </>
                ) : (
                  <Field label="Entries">
                    <textarea
                      required
                      rows="10"
                      value={editor.text}
                      onChange={(e) => edit("text", e.target.value)}
                    />
                  </Field>
                )}
                <Field label="Refresh interval">
                  <input
                    required
                    value={editor.refresh}
                    onChange={(e) => edit("refresh", e.target.value)}
                  />
                </Field>
                <label className="crawl-check">
                  <input
                    type="checkbox"
                    checked={editor.enabled}
                    onChange={(e) => edit("enabled", e.target.checked)}
                  />
                  Enabled
                </label>
                <button disabled={busy} type="submit">
                  Preview list
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setEditor(null);
                    setPreview(null);
                  }}
                >
                  Cancel
                </button>
                {preview && (
                  <div role="region" aria-label="List preview">
                    {preview.load_error && (
                      <p role="alert">{preview.load_error}</p>
                    )}
                    <p>
                      {preview.entries} entries · {preview.added} added ·{" "}
                      {preview.removed} removed
                    </p>
                    <p>
                      Recorded URLs: {preview.recorded_matches?.urls ?? 0} ·
                      Hosts: {preview.recorded_matches?.hosts ?? 0} · Coverage:{" "}
                      {preview.recorded_matches?.partial
                        ? "Partial"
                        : "Recorded"}
                    </p>
                    {preview.purges_existing_data && (
                      <p>Activation deletes matching stored records.</p>
                    )}
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() =>
                        perform(async () => {
                          setState(
                            await call("apply", {
                              revision: state.revision,
                              token: preview.token,
                              confirmed: true,
                            }),
                          );
                          setEditor(null);
                          setPreview(null);
                        })
                      }
                    >
                      {preview.load_error
                        ? "Save inactive feed"
                        : preview.purges_existing_data
                          ? "Activate and purge"
                          : "Save list"}
                    </button>
                  </div>
                )}
              </form>
            )}
            <form
              onSubmit={(e) => {
                e.preventDefault();
                perform(async () =>
                  setMatches((await call("match", { query })).sources),
                );
              }}
            >
              <Field label="Check URL">
                <input
                  type="url"
                  required
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                />
              </Field>
              <button disabled={busy}>Check matches</button>
            </form>
            {matches && (
              <p role="status">
                {matches.length
                  ? matches
                      .map(
                        (id) =>
                          state.lists.find((l) => l.id === id)?.name || id,
                      )
                      .join(", ")
                  : "No matches"}
              </p>
            )}
          </>
        ) : (
          <>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                perform(async () =>
                  setState(
                    await call("rules", {
                      revision: state.revision,
                      rules: state.rules,
                    }),
                  ),
                );
              }}
            >
              {state.rules.map((r, n) => (
                <fieldset key={r.id}>
                  <legend>{r.name || r.id}</legend>
                  <Field label={`Rule name ${n + 1}`}>
                    <input
                      required
                      value={r.name}
                      onChange={(e) =>
                        setState((s) => ({
                          ...s,
                          rules: s.rules.map((x, i) =>
                            i === n ? { ...x, name: e.target.value } : x,
                          ),
                        }))
                      }
                    />
                  </Field>
                  <label className="crawl-check">
                    <input
                      type="checkbox"
                      checked={r.enabled}
                      onChange={(e) =>
                        setState((s) => ({
                          ...s,
                          rules: s.rules.map((x, i) =>
                            i === n ? { ...x, enabled: e.target.checked } : x,
                          ),
                        }))
                      }
                    />
                    Enabled {r.name || r.id}
                  </label>
                  <Field label={`Phrases ${n + 1}`}>
                    <textarea
                      required
                      value={r.phrases.join("\n")}
                      onChange={(e) =>
                        setState((s) => ({
                          ...s,
                          rules: s.rules.map((x, i) =>
                            i === n
                              ? { ...x, phrases: e.target.value.split("\n") }
                              : x,
                          ),
                        }))
                      }
                    />
                  </Field>
                  <button
                    type="button"
                    onClick={() =>
                      setState((s) => ({
                        ...s,
                        rules: s.rules.filter((_, i) => i !== n),
                      }))
                    }
                  >
                    Delete rule {n + 1}
                  </button>
                </fieldset>
              ))}
              <button
                type="button"
                onClick={() =>
                  setState((s) => ({
                    ...s,
                    rules: [
                      ...s.rules,
                      {
                        id: crypto.randomUUID(),
                        name: "",
                        enabled: true,
                        phrases: [""],
                      },
                    ],
                  }))
                }
              >
                Add rule
              </button>
              <button disabled={busy}>Save rules</button>
            </form>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                perform(async () =>
                  setState(
                    await call("memory", {
                      revision: state.revision,
                      inspection_memory_mib: state.inspection_memory_mib,
                    }),
                  ),
                );
              }}
            >
              <Field label="Inspection memory budget (MiB)">
                <input
                  required
                  type="number"
                  min="32"
                  max="2048"
                  value={state.inspection_memory_mib}
                  onChange={(e) =>
                    setState((s) => ({
                      ...s,
                      inspection_memory_mib:
                        e.target.value === "" ? "" : Number(e.target.value),
                    }))
                  }
                />
              </Field>
              <button disabled={busy}>Apply memory budget</button>
            </form>
          </>
        )}
        <h3>Incidents</h3>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            perform(async () => {
              const rows = await call("incidents", { query: incidentSearch });
              setState((s) => ({ ...s, incidents: rows }));
            });
          }}
        >
          <Field label="Search incidents">
            <input
              value={incidentSearch}
              onChange={(e) => setIncidentSearch(e.target.value)}
            />
          </Field>
          <button disabled={busy}>Search</button>
        </form>
        <ul>
          {state.incidents.map((i) => (
            <li key={i.id}>
              <button
                onClick={() =>
                  perform(async () => {
                    setIncidentOffset(0);
                    setIncident(
                      await call("incident", { id: i.id, offset: 0 }),
                    );
                  })
                }
              >
                {i.url || i.source_name || i.rule_id} · {i.state}
              </button>
            </li>
          ))}
        </ul>
        {incident && (
          <section aria-label="Incident report">
            <h3>Incident report</h3>
            <dl>
              {[
                ["URL", incident.url],
                ["Source", incident.source_name || incident.rule_id],
                ["Source ID", incident.rule_id],
                [
                  "Kind",
                  incident.kind === "restricted"
                    ? "Suspected restricted-content indicator"
                    : "Blocklist match",
                ],
                ["Status", incident.state],
                ["Purge depth", incident.depth],
                ["Graph revision", incident.graph_revision],
                ["Historical links", incident.partial ? "Partial" : "Recorded"],
              ].map(([k, v]) => (
                <React.Fragment key={k}>
                  <dt>{k}</dt>
                  <dd>{v}</dd>
                </React.Fragment>
              ))}
            </dl>
            <ul>
              {incident.targets.map((t) => (
                <li key={t.site}>
                  {t.site} · distance {t.distance} ·{" "}
                  {t.distance
                    ? "Blocked by purge depth"
                    : incident.kind === "restricted"
                      ? "Detected site"
                      : "Blocklist match"}
                </li>
              ))}
            </ul>
            {incident.error && <p role="alert">{incident.error}</p>}
            {incident.state === "failed" && (
              <button
                disabled={busy}
                onClick={() =>
                  perform(async () => {
                    setState(await call("retry", { id: incident.id }));
                    setIncident({ ...incident, state: "pending", error: "" });
                  })
                }
              >
                Retry purge
              </button>
            )}
            <dl>
              {Object.entries(incident.progress || {}).map(([key, value]) => (
                <React.Fragment key={key}>
                  <dt>{key}</dt>
                  <dd>{value}</dd>
                </React.Fragment>
              ))}
            </dl>
            {incident.kind === "restricted" && (
              <p>{incident.target_count} selected sites</p>
            )}
            <button
              disabled={busy || incidentOffset === 0}
              onClick={() =>
                perform(async () => {
                  const offset = Math.max(0, incidentOffset - 200);
                  setIncident(
                    await call("incident", { id: incident.id, offset }),
                  );
                  setIncidentOffset(offset);
                })
              }
            >
              Previous sites
            </button>
            <button
              disabled={
                busy ||
                incidentOffset + incident.targets.length >=
                  incident.target_count
              }
              onClick={() =>
                perform(async () => {
                  const offset = incidentOffset + 200;
                  setIncident(
                    await call("incident", { id: incident.id, offset }),
                  );
                  setIncidentOffset(offset);
                })
              }
            >
              Next sites
            </button>
            <a
              href={`/workspace/protection/report?id=${encodeURIComponent(incident.id)}`}
              download
            >
              Export report
            </a>
          </section>
        )}
      </fieldset>
    </section>
  );
}
