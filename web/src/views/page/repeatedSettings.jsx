import React, { useState } from "react";
import { Field } from "./crawlEditor";
import { crawlAPI } from "./crawlApi";
export const repeatedDefaults = {
  mode: "deprioritize",
  minimum_sites: 3,
  structure_similarity: 0.95,
  text_similarity: 0.85,
  destination_similarity: 0.8,
  host_dominance: 0.8,
  site_window: 20,
  family_window: 100,
  max_text_kib: 256,
  max_destinations: 256,
  admission_share_percent: 10,
};
export const repeatedFields = [
  ["Independent sites", "minimum_sites", 3, 1000, 1],
  ["Repeated structure similarity", "structure_similarity", 0.5, 1, 0.01],
  ["Visible-text similarity", "text_similarity", 0.5, 1, 0.01],
  ["Outgoing-destination similarity", "destination_similarity", 0.5, 1, 0.01],
  ["Host family dominance", "host_dominance", 0.5, 1, 0.01],
  ["Pages per site", "site_window", 3, 200, 1],
  ["Pages per family", "family_window", 3, 1000, 1],
  ["Visible text limit (KiB)", "max_text_kib", 4, 1024, 1],
  ["Distinct destination limit", "max_destinations", 16, 4096, 1],
  ["Repeated admission share (%)", "admission_share_percent", 1, 50, 1],
];
export function RepeatedSettings({ value, onChange, custom = false }) {
  const settings = {
    ...repeatedDefaults,
    ...(custom && !value ? { mode: "off" } : value),
  };
  const set = (key, value) => onChange({ ...settings, [key]: value });
  return (
    <details>
      <summary>Repeated pages</summary>
      <label>
        Repeated-page mode
        <select
          aria-label="Repeated-page mode"
          value={settings.mode}
          onChange={(e) => set("mode", e.target.value)}
        >
          <option value="off">Off</option>
          <option value="observe">Observe</option>
          <option value="deprioritize">Deprioritize</option>
        </select>
      </label>
      {settings.mode !== "off" && (
        <div className="crawl-pair">
          {repeatedFields.map(([label, key, min, max, step]) => (
            <Field key={key} label={label}>
              <input
                required
                type="number"
                min={min}
                max={max}
                step={step}
                value={settings[key]}
                onChange={(e) =>
                  set(key, e.target.value === "" ? "" : Number(e.target.value))
                }
              />
            </Field>
          ))}
        </div>
      )}
    </details>
  );
}
export function ExpertRepeatedSettings({
  pipeline,
  onApply,
  onError,
  onBusy,
  disabled,
}) {
  const [draft, setDraft] = useState({}),
    [busy, setBusy] = useState(false);
  let resources;
  try {
    resources =
      JSON.parse(pipeline).resources?.filter(
        (r) => r.type === "frontier_store",
      ) || [];
  } catch {
    return null;
  }
  if (!resources.length) return null;
  return (
    <section>
      {resources.map((r) => (
        <div key={r.name}>
          <h3>{r.name}</h3>
          <RepeatedSettings
            custom
            value={draft[r.name] ?? r.parameters?.repeated_pages}
            onChange={(v) => setDraft((old) => ({ ...old, [r.name]: v }))}
          />
        </div>
      ))}
      {Object.keys(draft).length > 0 && (
        <button
          type="button"
          disabled={disabled || busy}
          onClick={async () => {
            setBusy(true);
            onBusy?.(true);
            try {
              const result = await crawlAPI("patch_repeated_pages", {
                configuration_json: pipeline,
                settings: draft,
              });
              onApply(result.configuration_json);
              setDraft({});
            } catch (e) {
              onError(e.message);
            } finally {
              setBusy(false);
              onBusy?.(false);
            }
          }}
        >
          Apply repeated-page settings
        </button>
      )}
    </section>
  );
}
