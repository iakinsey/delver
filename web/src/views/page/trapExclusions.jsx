import React, { useState } from "react";
import { crawlAPI } from "./crawlApi";
export function TrapExclusions({ crawlId, onError }) {
  const [rows, setRows] = useState(null),
    [busy, setBusy] = useState(false);
  async function load(action = "trap_decisions", site) {
    setBusy(true);
    onError("");
    try {
      setRows(await crawlAPI(action, { crawl_id: crawlId, site }));
    } catch (e) {
      onError(e.message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <details
      onToggle={(e) => {
        if (e.currentTarget.open && rows === null) load();
      }}
    >
      <summary>Trap exclusions</summary>
      <button disabled={busy} onClick={() => load()}>
        Reload exclusions
      </button>
      {rows && (
        <ul>
          {rows.map((d) => (
            <li key={d.site}>
              <strong>{d.site}</strong>
              <dl>
                {[
                  ["Reason", d.reason],
                  ["Samples", d.samples],
                  ["Sites", d.sites],
                  ["Structure similarity", d.similarity],
                  ["Link redundancy", d.redundancy],
                  ["Discovery novelty", d.novelty],
                  ["Fresh links", d.fresh_links],
                  ["Cross-site links", d.cross_site_links],
                ].map(([k, v]) => (
                  <React.Fragment key={k}>
                    <dt>{k}</dt>
                    <dd>{v}</dd>
                  </React.Fragment>
                ))}
              </dl>
              <button
                disabled={busy}
                onClick={() => load("trap_restore", d.site)}
              >
                Restore {d.site}
              </button>
            </li>
          ))}
        </ul>
      )}
    </details>
  );
}
