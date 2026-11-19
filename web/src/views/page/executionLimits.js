import React, { useState, useEffect } from "react";
import { workspaceAPI } from "./crawlApi";
import { Field } from "./crawlEditor";

export function ExecutionLimits({ onError, onChange }) {
  const [settings, setSettings] = useState(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    load();
  }, []);
  const set = (key, value) =>
    setSettings((old) => ({
      ...old,
      execution: { ...old.execution, [key]: value },
    }));
  async function load() {
    setBusy(true);
    try {
      const next = await workspaceAPI("settings");
      setSettings(next);
      onChange?.(next.execution);
      onError("");
    } catch (e) {
      onError(e.message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="crawl-panel">
      <h2>Execution limits</h2>
      {settings && (
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            onError("");
            try {
              const next = await workspaceAPI("settings", settings);
              setSettings(next);
              onChange?.(next.execution);
            } catch (e) {
              onError(e.message);
            } finally {
              setBusy(false);
            }
          }}
        >
          <div className="crawl-pair">
            <Field label="Process concurrency">
              <input
                required
                type="number"
                min="1"
                max="8192"
                disabled={busy}
                value={settings.execution.concurrency}
                onChange={(e) =>
                  set(
                    "concurrency",
                    e.target.value === "" ? "" : Number(e.target.value),
                  )
                }
              />
            </Field>
            <Field label="Shared host concurrency">
              <input
                required
                type="number"
                min="1"
                max="8192"
                disabled={busy}
                value={settings.execution.host_concurrency}
                onChange={(e) =>
                  set(
                    "host_concurrency",
                    e.target.value === "" ? "" : Number(e.target.value),
                  )
                }
              />
            </Field>
            <Field label="Shared host delay">
              <input
                required
                disabled={busy}
                value={settings.execution.host_delay}
                onChange={(e) => set("host_delay", e.target.value)}
              />
            </Field>
          </div>
          <button disabled={busy} type="submit">
            Apply limits
          </button>
          <button disabled={busy} type="button" onClick={load}>
            Reload
          </button>
        </form>
      )}
      {!settings && !busy && <button onClick={load}>Load</button>}
    </section>
  );
}
