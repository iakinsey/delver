import React, { useState } from "react";
import { workspaceAPI } from "./crawlApi";
import { Field } from "./crawlEditor";

export function ExecutionLimits({ onError }) {
  const [settings, setSettings] = useState(null);
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState(false);
  const set = (key, value) =>
    setSettings((old) => ({
      ...old,
      execution: { ...old.execution, [key]: value },
    }));
  async function load() {
    setBusy(true);
    try {
      setSettings(await workspaceAPI("settings"));
      onError("");
    } catch (e) {
      onError(e.message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <details
      className="crawl-panel"
      open={open}
      onToggle={(e) => {
        setOpen(e.currentTarget.open);
        if (e.currentTarget.open && !settings && !busy) load();
      }}
    >
      <summary>Execution limits</summary>
      {settings && (
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            onError("");
            try {
              setSettings(await workspaceAPI("settings", settings));
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
    </details>
  );
}
