import { TrapSettings } from "./trapSettings";
import { FetchSettings, ExpertFetchSettings } from "./fetchSettings";
import React, { useState, useRef, useId, useEffect } from "react";
import { crawlAPI, strategyTitle, words } from "./crawlApi";
function Choice({ group, value, selected, title, children, onChange }) {
  return (
    <label className={`crawl-choice ${selected === value ? "selected" : ""}`}>
      <input
        type="radio"
        name={group}
        value={value}
        checked={selected === value}
        onChange={() => onChange(value)}
      />
      <strong>{title}</strong>
      {children && <span>{children}</span>}
    </label>
  );
}
function Lifetime({ label, value, onChange }) {
  const id = useId(),
    previous = useRef(value === "never" ? "7d" : value);
  return (
    <div className="crawl-lifetime">
      <label htmlFor={id}>{label}</label>
      <div>
        <input
          id={id}
          value={value === "never" ? "" : value}
          disabled={value === "never"}
          placeholder="7d"
          onChange={(e) => {
            previous.current = e.target.value;
            onChange(e.target.value);
          }}
        />
        <label className="crawl-check">
          <input
            type="checkbox"
            aria-label={`${label}: no expiry`}
            checked={value === "never"}
            onChange={(e) =>
              onChange(e.target.checked ? "never" : previous.current)
            }
          />
          No expiry
        </label>
      </div>
    </div>
  );
}
export function Field({ label, children }) {
  const id = useId();
  return (
    <div className="crawl-field">
      <label htmlFor={id}>{label}</label>
      {React.Children.map(children, (child) =>
        React.isValidElement(child) &&
        ["input", "textarea"].includes(child.type)
          ? React.cloneElement(child, { id })
          : child,
      )}
    </div>
  );
}
export function CrawlEditor({
  editor,
  setEditor,
  busy,
  onError,
  onCancel,
  onSave,
  execution,
}) {
  const d = editor.definition,
    [seeds, setSeeds] = useState(d.seeds.join("\n")),
    [feeds, setFeeds] = useState(d.feeds.join("\n")),
    [url, setURL] = useState(""),
    [preview, setPreview] = useState(null),
    [fetchApplying, setFetchApplying] = useState(false);
  const network = {
    respect_robots: true,
    request_timeout: "10s",
    max_retries: 1,
    retry_delay: "100ms",
    retry_max_delay: "5s",
    user_agent: "delver pre-alpha",
    require_proxy: false,
    restrict_hosts: false,
    allowed_host_suffixes: [],
    isolate_proxy_sessions: false,
    ...d.network,
  };
  network.max_retries ??= 1;
  network.respect_robots ??= true;
  const [hostSuffixText, setHostSuffixText] = useState(
    (network.allowed_host_suffixes || []).join("\n"),
  );
  const previewVersion = useRef(0);
  useEffect(() => {
    previewVersion.current++;
    setPreview(null);
  }, [
    url,
    d.scope,
    d.seeds,
    d.feeds,
    d.allow_pattern,
    d.deny_pattern,
    d.excluded_extensions,
    d.network?.restrict_hosts,
    d.network?.allowed_host_suffixes,
  ]);
  const set = (patch) =>
      setEditor((e) => ({ ...e, definition: { ...e.definition, ...patch } })),
    terms = (patch) => set({ termination: { ...d.termination, ...patch } }),
    net = (patch) => set({ network: { ...network, ...patch } }),
    retention = (key, value) =>
      set({ retention: { ...d.retention, [key]: value } });
  const isExpert = editor.pipeline !== null,
    locked = busy || fetchApplying;
  return (
    <form
      className="crawl-editor"
      onSubmit={(e) => {
        e.preventDefault();
        if (!locked) onSave(false);
      }}
    >
      <section className="crawl-panel">
        <div className="crawl-heading">
          <h2>{editor.record ? "Edit crawl" : "Create crawl"}</h2>
          <button type="button" disabled={locked} onClick={onCancel}>
            Cancel
          </button>
        </div>
        <Field label="Name">
          <input
            required
            value={d.name}
            onChange={(e) => set({ name: e.target.value })}
          />
        </Field>
        {isExpert ? (
          <>
            <Field label="Worker pipeline JSON">
              <textarea
                rows={25}
                value={editor.pipeline}
                disabled={fetchApplying}
                spellCheck={false}
                onChange={(e) =>
                  setEditor((old) => ({ ...old, pipeline: e.target.value }))
                }
              />
            </Field>
            <ExpertFetchSettings
              pipeline={editor.pipeline}
              onBusy={setFetchApplying}
              onApply={(pipeline) => setEditor((old) => ({ ...old, pipeline }))}
              onError={onError}
            />
            <h3>Run completion</h3>
            <fieldset className="crawl-choices">
              <legend className="sr-only">Custom pipeline completion</legend>
              <Choice
                group="expert-completion"
                value="monitor"
                selected={d.termination.when_drained ? "drain" : "monitor"}
                title="Continuous"
                onChange={() => terms({ when_drained: false })}
              ></Choice>
              <Choice
                group="expert-completion"
                value="drain"
                selected={d.termination.when_drained ? "drain" : "monitor"}
                title="Drain"
                onChange={() => terms({ when_drained: true })}
              >
                Requires once:true source jobs. Disables scheduled revisits.
              </Choice>
            </fieldset>
            <div className="crawl-pair">
              <Field label="Fetch limit">
                <input
                  type="number"
                  min="0"
                  value={d.termination.max_requests || ""}
                  placeholder="No limit"
                  onChange={(e) =>
                    terms({ max_requests: Number(e.target.value) })
                  }
                />
              </Field>
              <Field label="Time limit">
                <input
                  value={d.termination.duration}
                  placeholder="e.g. 6h"
                  onChange={(e) => terms({ duration: e.target.value })}
                />
              </Field>
            </div>
            <p>
              <small>
                Fetch limits require a single frontier store and
                frontier-admitted sources.
              </small>
            </p>
          </>
        ) : (
          <>
            <h3>Sources</h3>
            <Field label="Seed URLs">
              <textarea
                rows={4}
                value={seeds}
                placeholder={"https://example.com/\nhttps://another-site.com/"}
                onChange={(e) => {
                  setSeeds(e.target.value);
                  set({ seeds: e.target.value.split(/\s+/).filter(Boolean) });
                }}
              />
            </Field>
            <details>
              <summary>RSS / Atom feeds</summary>
              <Field label="Feed URLs">
                <textarea
                  rows={3}
                  value={feeds}
                  onChange={(e) => {
                    setFeeds(e.target.value);
                    set({ feeds: e.target.value.split(/\s+/).filter(Boolean) });
                  }}
                />
              </Field>
            </details>
            <h3>Crawl scope</h3>
            <fieldset className="crawl-choices">
              <legend className="sr-only">Crawl boundaries</legend>
              {[
                ["same_origin", "Same host", ""],
                ["same_domain", "Same domain", "Includes subdomains."],
                ["open_web", "Open web", ""],
              ].map(([value, title, text]) => (
                <Choice
                  key={value}
                  group="scope"
                  value={value}
                  selected={d.scope}
                  title={title}
                  onChange={(scope) => set({ scope })}
                >
                  {text}
                </Choice>
              ))}
            </fieldset>
            <details>
              <summary>URL filters</summary>
              <Field label="Restricted-content purge depth">
                <input
                  required
                  type="number"
                  min="0"
                  step="1"
                  value={d.restricted_purge_depth ?? 0}
                  onChange={(e) =>
                    set({
                      restricted_purge_depth:
                        e.target.value === "" ? "" : Number(e.target.value),
                    })
                  }
                />
              </Field>
              <Field label="Excluded file extensions">
                <input
                  value={d.excluded_extensions || ""}
                  placeholder="zip, tar.gz, pdf"
                  onChange={(e) => set({ excluded_extensions: e.target.value })}
                />
              </Field>
              <Field label="Allow pattern">
                <input
                  value={d.allow_pattern}
                  placeholder="Optional regular expression"
                  onChange={(e) => set({ allow_pattern: e.target.value })}
                />
              </Field>
              <Field label="Deny pattern">
                <input
                  value={d.deny_pattern}
                  placeholder="/logout|/calendar/"
                  onChange={(e) => set({ deny_pattern: e.target.value })}
                />
              </Field>
              <Field label="Test URL">
                <input
                  value={url}
                  onChange={(e) => {
                    setURL(e.target.value);
                    setPreview(null);
                  }}
                />
              </Field>
              <button
                type="button"
                disabled={!url}
                onClick={async () => {
                  try {
                    const version = previewVersion.current;
                    const rows = await crawlAPI("preview", {
                      definition: d,
                      urls: [url],
                    });
                    if (version === previewVersion.current) {
                      setPreview(rows[0]);
                      onError("");
                    }
                  } catch (e) {
                    onError(e.message);
                  }
                }}
              >
                Check URL
              </button>
              {preview && (
                <p role="status">
                  {preview.reason ||
                    (preview.allowed ? "Allowed" : "Excluded")}{" "}
                </p>
              )}
              <label className="crawl-check">
                <input
                  type="checkbox"
                  checked={network.restrict_hosts}
                  onChange={(e) => net({ restrict_hosts: e.target.checked })}
                />
                Restrict hostnames
              </label>
              {network.restrict_hosts && (
                <>
                  <Field label="Allowed hostname suffixes">
                    <textarea
                      rows={3}
                      value={hostSuffixText}
                      placeholder={"internal.example\nprivate.example"}
                      onChange={(e) => {
                        setHostSuffixText(e.target.value);
                        net({
                          allowed_host_suffixes: e.target.value
                            .split(/[\s,]+/)
                            .filter(Boolean),
                        });
                      }}
                    />
                  </Field>
                  <p>
                    <small>
                      Includes subdomains. Applies to all requests and
                      redirects.
                    </small>
                  </p>
                </>
              )}
              <div className="crawl-pair">
                <Field label="Maximum depth">
                  <input
                    type="number"
                    min="1"
                    value={d.max_depth}
                    onChange={(e) => set({ max_depth: Number(e.target.value) })}
                  />
                </Field>
                <Field label="Maximum host hops">
                  <input
                    type="number"
                    min="1"
                    value={d.max_hops}
                    onChange={(e) => set({ max_hops: Number(e.target.value) })}
                  />
                </Field>
              </div>
            </details>
            <TrapSettings
              value={d.traps}
              onChange={(traps) => set({ traps })}
            />
            <h3>Traversal strategy</h3>
            <fieldset className="crawl-choices">
              <legend className="sr-only">Traversal strategy</legend>
              {[
                ["host_diverse", "Host-diverse", "Rotate hosts."],
                ["breadth_first", "Breadth-first", ""],
                ["depth_first", "Depth-first", ""],
                ["adaptive", "Adaptive", "Prioritize unique HTML yield."],
              ].map(([value, title, text]) => (
                <Choice
                  key={value}
                  group="strategy"
                  value={value}
                  selected={d.strategy}
                  title={title}
                  onChange={(strategy) => set({ strategy })}
                >
                  {text}
                </Choice>
              ))}
            </fieldset>
            <h3>Run completion</h3>
            <fieldset className="crawl-choices">
              <legend className="sr-only">Run behavior</legend>
              <Choice
                group="completion"
                value="monitor"
                selected={d.termination.when_drained ? "drain" : "monitor"}
                title="Monitor"
                onChange={() => terms({ when_drained: false })}
              >
                Scheduled revisits.
              </Choice>
              <Choice
                group="completion"
                value="drain"
                selected={d.termination.when_drained ? "drain" : "monitor"}
                title="Drain"
                onChange={() => terms({ when_drained: true })}
              >
                Read sources once; complete after queues drain.
              </Choice>
            </fieldset>
            {!d.termination.when_drained && (
              <Field label="Revisit interval">
                <input
                  value={d.revisit}
                  onChange={(e) => set({ revisit: e.target.value })}
                />
              </Field>
            )}
            <details>
              <summary>Run limits</summary>
              <div className="crawl-pair">
                <Field label="Fetch limit">
                  <input
                    type="number"
                    min="0"
                    placeholder="No limit"
                    value={d.termination.max_requests || ""}
                    onChange={(e) =>
                      terms({ max_requests: Number(e.target.value) })
                    }
                  />
                </Field>
                <Field label="Time limit">
                  <input
                    placeholder="e.g. 6h"
                    value={d.termination.duration}
                    onChange={(e) => terms({ duration: e.target.value })}
                  />
                </Field>
              </div>
            </details>
            <h3>Connection</h3>
            <fieldset className="crawl-choices">
              <legend className="sr-only">Proxy connection</legend>
              {[
                ["direct", "Direct", ""],
                ["http", "HTTP / HTTPS", ""],
                ["socks5", "SOCKS5", ""],
              ].map(([value, title, text]) => (
                <Choice
                  key={value}
                  group="proxy"
                  value={value}
                  selected={d.proxy?.mode || "direct"}
                  title={title}
                  onChange={(mode) =>
                    set({
                      proxy: { mode, url: "" },
                      network: {
                        ...network,
                        isolate_proxy_sessions:
                          mode === "socks5" && network.isolate_proxy_sessions,
                      },
                    })
                  }
                >
                  {text}
                </Choice>
              ))}
            </fieldset>
            {d.proxy?.mode && d.proxy.mode !== "direct" && (
              <Field label="Proxy address">
                <input
                  type="text"
                  aria-label="Proxy address"
                  autoComplete="off"
                  value={d.proxy.url}
                  placeholder={
                    d.proxy.mode === "http"
                      ? "http://user:password@proxy:8080"
                      : "socks5://user:password@proxy:1080"
                  }
                  onChange={(e) =>
                    set({ proxy: { ...d.proxy, url: e.target.value } })
                  }
                />
              </Field>
            )}
            <label className="crawl-check">
              <input
                type="checkbox"
                checked={network.require_proxy}
                onChange={(e) => net({ require_proxy: e.target.checked })}
              />
              Require proxy
            </label>
            {d.proxy?.mode === "socks5" && (
              <>
                <label className="crawl-check">
                  <input
                    type="checkbox"
                    checked={network.isolate_proxy_sessions}
                    onChange={(e) =>
                      net({ isolate_proxy_sessions: e.target.checked })
                    }
                  />
                  Isolate proxy sessions
                </label>
                {network.isolate_proxy_sessions && (
                  <p>
                    <small>
                      Generates SOCKS credentials per run. Requires proxy
                      support and a URL without credentials.
                    </small>
                  </p>
                )}
              </>
            )}
            <label className="crawl-check">
              <input
                type="checkbox"
                checked={network.respect_robots}
                onChange={(e) => net({ respect_robots: e.target.checked })}
              />
              Respect robots.txt
            </label>
            <details>
              <summary>Requests</summary>
              <div className="crawl-pair">
                <Field label="Request timeout">
                  <input
                    placeholder="10s"
                    value={network.request_timeout}
                    onChange={(e) => net({ request_timeout: e.target.value })}
                  />
                </Field>
                <Field label="Retries per request">
                  <input
                    type="number"
                    min="0"
                    max="10"
                    value={network.max_retries}
                    onChange={(e) =>
                      net({ max_retries: Number(e.target.value) })
                    }
                  />
                </Field>
                <Field label="Initial retry delay">
                  <input
                    placeholder="100ms"
                    value={network.retry_delay}
                    onChange={(e) => net({ retry_delay: e.target.value })}
                  />
                </Field>
                <Field label="Maximum retry delay">
                  <input
                    placeholder="5s"
                    value={network.retry_max_delay}
                    onChange={(e) => net({ retry_max_delay: e.target.value })}
                  />
                </Field>
              </div>
              <Field label="User-Agent">
                <input
                  placeholder="User-Agent"
                  value={network.user_agent}
                  onChange={(e) => net({ user_agent: e.target.value })}
                />
              </Field>
              <p>
                <small>
                  Timeout excludes pacing. Retry-After overrides retry delay.
                </small>
              </p>
            </details>
            <FetchSettings
              value={d.fetch}
              onChange={(fetch) => set({ fetch })}
              onError={onError}
            />
            {execution && d.fetch && (
              <p>
                Effective concurrency ceiling:{" "}
                {Math.min(
                  d.fetch.mode === "adaptive"
                    ? d.fetch.maximum
                    : d.fetch.concurrency,
                  execution.concurrency,
                )}{" "}
                · Process ceiling: {execution.concurrency}
              </p>
            )}
            <details>
              <summary>Request pacing</summary>
              <p>
                {execution
                  ? `Effective host concurrency: ${Math.min(Number(d.concurrency) || 1, execution.host_concurrency)} · Shared ceiling: ${execution.host_concurrency} · Shared delay: ${execution.host_delay}`
                  : ""}
              </p>
              <div className="crawl-pair">
                <Field label="Request delay">
                  <input
                    value={d.request_delay}
                    onChange={(e) => set({ request_delay: e.target.value })}
                  />
                </Field>
                <Field label="Per-host concurrency">
                  <input
                    required
                    type="number"
                    min="1"
                    max="8192"
                    value={d.concurrency}
                    onChange={(e) =>
                      set({ concurrency: Number(e.target.value) })
                    }
                  />
                </Field>
              </div>
            </details>
            <h3>Retention</h3>
            <Lifetime
              label="Completed records"
              value={d.retention.completed_for}
              onChange={(v) => retention("completed_for", v)}
            />
            <Lifetime
              label="Failed records"
              value={d.retention.failed_for}
              onChange={(v) => retention("failed_for", v)}
            />
            <Lifetime
              label="URL deduplication"
              value={d.retention.dedup_for}
              onChange={(v) => retention("dedup_for", v)}
            />
            <small>
              Deduplication retention must equal or exceed record retention.
            </small>
            {editor.record && (
              <details>
                <summary>Worker pipeline</summary>
                <p>Saves the draft and switches to pipeline JSON.</p>
                <button
                  type="button"
                  onClick={async () => {
                    try {
                      const saved = await crawlAPI("draft", {
                        crawl_id: editor.record.id,
                        revision: editor.record.revision,
                        definition: d,
                      });
                      setEditor((e) => ({ ...e, record: saved }));
                      const r = await crawlAPI("pipeline", {
                        crawl_id: saved.id,
                      });
                      setEditor((e) => ({
                        ...e,
                        pipeline:
                          r.configuration_json ||
                          JSON.stringify(r.configuration, null, 2),
                      }));
                    } catch (e) {
                      onError(e.message);
                    }
                  }}
                >
                  Edit pipeline JSON
                </button>
              </details>
            )}
          </>
        )}
      </section>
      <aside className="crawl-review crawl-panel">
        <h2>{d.name || "Untitled crawl"}</h2>
        {isExpert ? (
          <p>Custom pipeline</p>
        ) : (
          <>
            {" "}
            <p>
              Sources: <strong>{d.seeds.length + d.feeds.length}</strong>
            </p>
            <p>
              {d.scope === "open_web"
                ? `Scope: Open web · Host hops: ${d.max_hops}`
                : d.scope === "same_origin"
                  ? "Scope: Same host"
                  : "Scope: Same domain"}
            </p>
            <p>
              {strategyTitle(d.strategy)} · Depth: {d.max_depth}
            </p>
          </>
        )}
        <p>
          {d.termination.when_drained
            ? "Completion: Drain"
            : isExpert
              ? "Completion: Continuous"
              : `Revisit interval: ${d.revisit}`}
        </p>
        {d.termination.max_requests > 0 && (
          <p>Fetch limit: {d.termination.max_requests}</p>
        )}
        {d.termination.duration && <p>Time limit: {d.termination.duration}</p>}
        {!isExpert && (
          <>
            {" "}
            <p>
              {d.proxy?.mode && d.proxy.mode !== "direct"
                ? `Proxy: ${d.proxy.mode.toUpperCase()}`
                : "Proxy: Direct"}
            </p>
          </>
        )}
        <hr />
        <button
          className="crawl-primary"
          type="button"
          disabled={locked}
          onClick={() => onSave(true)}
        >
          {editor.record ? "Save and restart" : "Create and start"}
        </button>
        <button type="submit" disabled={locked}>
          {editor.record ? "Save draft" : "Create"}
        </button>
      </aside>
    </form>
  );
}
