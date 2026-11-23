import React, { useState } from "react";
import options from "./fetchOptions.json";
import { crawlAPI } from "./crawlApi";

export const adaptiveOption = (key) =>
  [
    "adjustment_interval",
    "minimum",
    "maximum",
    "min_samples",
    "increase_step",
    "decrease_factor",
    "overload_failure_ratio",
    "cpu_high_percent",
    "cpu_low_percent",
  ].includes(key);

export function FetchSettings({ value, onChange, onError, disabled = false }) {
  if (!value)
    return (
      <section>
        <h3>Fetch throughput</h3>
        <button
          type="button"
          disabled={disabled}
          onClick={async () => {
            try {
              const defaults = await crawlAPI("defaults");
              onChange({ ...defaults.fetch, concurrency: 4 });
            } catch (error) {
              onError(error.message);
            }
          }}
        >
          Configure
        </button>
      </section>
    );
  const set = (key, v) => onChange({ ...value, [key]: v });
  const visibleOptions = options.filter(
    (o) => value.mode === "adaptive" || !adaptiveOption(o.key),
  );
  const groups = [
    ...new Set(visibleOptions.map((option) => option.group)),
  ].sort((a, b) => {
    const order = [
      "Execution",
      "Adaptive limits",
      "Adaptive control",
      "Resource limits",
      "Dispatch and backpressure",
      "Retries and recovery",
      "Network phases",
      "Metrics",
    ];
    const rank = (v) => order.indexOf(v);
    return rank(a) - rank(b);
  });
  return (
    <section className="fetch-settings">
      <h3>Fetch throughput</h3>
      <fieldset className="crawl-choices">
        <legend>Concurrency control</legend>
        {["fixed", "adaptive"].map((mode) => (
          <label
            key={mode}
            className={`crawl-choice ${value.mode === mode ? "selected" : ""}`}
          >
            <input
              type="radio"
              disabled={disabled}
              name="fetch-mode"
              value={mode}
              checked={value.mode === mode}
              onChange={() => set("mode", mode)}
            />
            <strong>
              {mode === "fixed" ? "Fixed concurrency" : "Adaptive concurrency"}
            </strong>
          </label>
        ))}
      </fieldset>
      {groups.map((group) => (
        <details key={group} open={group === "Execution" || undefined}>
          <summary>{group}</summary>
          <div className="crawl-pair">
            {visibleOptions
              .filter((option) => option.group === group)
              .map((option) => (
                <label className="crawl-field" key={option.key}>
                  <span>{option.label}</span>
                  <input
                    aria-label={option.label}
                    disabled={disabled}
                    type={option.kind.includes("duration") ? "text" : "number"}
                    min={option.min}
                    max={option.max}
                    step={
                      option.kind === "integer"
                        ? 1
                        : option.kind === "number"
                          ? "any"
                          : undefined
                    }
                    value={value[option.key] ?? ""}
                    placeholder={
                      option.kind === "inherit_duration" ? "inherit" : undefined
                    }
                    onChange={(event) =>
                      set(
                        option.key,
                        option.kind.includes("duration")
                          ? event.target.value
                          : event.target.value === ""
                            ? ""
                            : Number(event.target.value),
                      )
                    }
                  />
                </label>
              ))}
          </div>
        </details>
      ))}
      <small>
        Shared limits: lowest concurrency, longest host delay. Phase timeout 0s
        inherits the request timeout.
      </small>
    </section>
  );
}

export function ExpertFetchSettings({
  pipeline,
  onApply,
  onError,
  onBusy,
  disabled = false,
}) {
  const [draft, setDraft] = useState(null);
  const [applying, setApplying] = useState(false);
  const setBusy = (value) => {
    setApplying(value);
    onBusy?.(value);
  };
  let parsed;
  try {
    parsed = JSON.parse(pipeline);
  } catch {
    return null;
  }
  const value = draft || parsed.config?.fetch;
  return (
    <div>
      <FetchSettings
        value={value}
        onChange={setDraft}
        onError={onError}
        disabled={applying || disabled}
      />
      {draft && (
        <button
          type="button"
          disabled={applying || disabled}
          onClick={async () => {
            setBusy(true);
            try {
              const result = await crawlAPI("patch_fetch", {
                configuration_json: pipeline,
                fetch: draft,
              });
              onApply(result.configuration_json);
              setDraft(null);
            } catch (error) {
              onError(error.message);
            } finally {
              setBusy(false);
            }
          }}
        >
          Apply
        </button>
      )}
    </div>
  );
}

export function ExpertQueueCacheSettings({
  pipeline,
  onApply,
  onError,
  onBusy,
  disabled = false,
}) {
  const [draft, setDraft] = useState({});
  const [applying, setApplying] = useState(false);
  let resources;
  try {
    resources = (JSON.parse(pipeline).resources || []).filter(
      (r) => r.type === "crawl_queue",
    );
  } catch {
    return null;
  }
  if (!resources.length) return null;
  return (
    <section>
      <h3>Request queue storage</h3>
      {resources.map((r) => (
        <label key={r.name} className="crawl-field">
          Request queue cache (MiB): {r.name}
          <input
            type="number"
            min="16"
            max="1024"
            required
            disabled={applying || disabled}
            value={draft[r.name] ?? r.parameters?.cache_budget_mb ?? 64}
            onChange={(e) =>
              setDraft((old) => ({
                ...old,
                [r.name]: e.target.value === "" ? "" : Number(e.target.value),
              }))
            }
          />
        </label>
      ))}
      {Object.keys(draft).length > 0 && (
        <button
          type="button"
          disabled={applying || disabled}
          onClick={async () => {
            setApplying(true);
            onBusy?.(true);
            try {
              const result = await crawlAPI("patch_queue_cache", {
                configuration_json: pipeline,
                budgets: draft,
              });
              onApply(result.configuration_json);
              setDraft({});
            } catch (e) {
              onError(e.message);
            } finally {
              setApplying(false);
              onBusy?.(false);
            }
          }}
        >
          Apply queue cache
        </button>
      )}
    </section>
  );
}
