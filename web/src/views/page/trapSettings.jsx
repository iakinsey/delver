import React from "react";
import { Field } from "./crawlEditor";
export const trapDefaults = {
  enabled: true,
  automatic: true,
  site_window: 20,
  family_window: 100,
  similarity: 0.7,
  redundancy: 0.8,
  novelty: 0.05,
  minimum_samples: 5,
  minimum_links: 8,
  fresh_links: 0.5,
  cross_site_links: 0.5,
};
export function TrapSettings({ value, onChange }) {
  const settings = value || { ...trapDefaults, enabled: false };
  const set = (key, v) => onChange({ ...settings, [key]: v });
  return (
    <details>
      <summary>Trap detection</summary>
      <label className="crawl-check">
        <input
          type="checkbox"
          checked={settings.enabled}
          onChange={(e) => set("enabled", e.target.checked)}
        />
        Detect crawler traps
      </label>
      {settings.enabled && (
        <>
          <label className="crawl-check">
            <input
              type="checkbox"
              checked={settings.automatic}
              onChange={(e) => set("automatic", e.target.checked)}
            />
            Automatically exclude detected traps
          </label>
          <div className="crawl-pair">
            {[
              ["Site observation window", "site_window", 3, 200, 1],
              ["Family observation window", "family_window", 3, 1000, 1],
              ["Structure similarity", "similarity", 0.5, 1, 0.01],
              ["Link pattern redundancy", "redundancy", 0.5, 1, 0.01],
              ["Maximum discovery novelty", "novelty", 0, 0.5, 0.01],
              ["Minimum fresh-link fraction", "fresh_links", 0, 1, 0.01],
              [
                "Minimum cross-site link fraction",
                "cross_site_links",
                0,
                1,
                0.01,
              ],
              ["Samples per confirmation window", "minimum_samples", 2, 100, 1],
              ["Minimum links per page", "minimum_links", 2, 10000, 1],
            ].map(([label, key, min, max, step]) => (
              <Field key={key} label={label}>
                <input
                  required
                  type="number"
                  min={min}
                  max={max}
                  step={step}
                  value={settings[key]}
                  onChange={(e) =>
                    set(
                      key,
                      e.target.value === "" ? "" : Number(e.target.value),
                    )
                  }
                />
              </Field>
            ))}
          </div>
        </>
      )}
    </details>
  );
}
