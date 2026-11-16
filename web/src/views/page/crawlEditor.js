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
      <span>{children}</span>
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
            aria-label={`${label}: keep forever`}
            checked={value === "never"}
            onChange={(e) =>
              onChange(e.target.checked ? "never" : previous.current)
            }
          />
          Keep forever
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
          <h2>{editor.record ? "Edit this crawl" : "Create a crawl"}</h2>
          <button type="button" disabled={locked} onClick={onCancel}>
            Discard changes
          </button>
        </div>
        <Field label="Crawl name">
          <input
            required
            value={d.name}
            onChange={(e) => set({ name: e.target.value })}
          />
        </Field>
        {isExpert ? (
          <>
            <p>
              This custom worker pipeline is preserved as-is. Saving never
              flattens it into guided settings.
            </p>
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
                title="Keep running"
                onChange={() => terms({ when_drained: false })}
              >
                Let the pipeline continue until stopped or a limit is reached.
              </Choice>
              <Choice
                group="expert-completion"
                value="drain"
                selected={d.termination.when_drained ? "drain" : "monitor"}
                title="Finish when the queue drains"
                onChange={() => terms({ when_drained: true })}
              >
                Source jobs must declare once:true; future frontier revisits are
                disabled.
              </Choice>
            </fieldset>
            <div className="crawl-pair">
              <Field label="Maximum page fetches">
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
              <Field label="Running-time limit">
                <input
                  value={d.termination.duration}
                  placeholder="No limit, or 6h"
                  onChange={(e) => terms({ duration: e.target.value })}
                />
              </Field>
            </div>
            <p>
              <small>
                Request budgets require one frontier store, with source requests
                admitted through the frontier. Custom sinks and worker settings
                remain intact.
              </small>
            </p>
          </>
        ) : (
          <>
            <h3>Start here</h3>
            <p>
              Paste the websites or pages you want to explore, one per line.
            </p>
            <Field label="Starting URLs">
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
              <summary>Add RSS or Atom feeds</summary>
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
              <p>
                Feed articles become starting pages for the selected boundaries.
              </p>
            </details>
            <h3>How far should it explore?</h3>
            <fieldset className="crawl-choices">
              <legend className="sr-only">Crawl boundaries</legend>
              {[
                [
                  "same_origin",
                  "Stay on these hosts",
                  "Follow pages on each starting host. Keep subdomains separate.",
                ],
                [
                  "same_domain",
                  "Explore each website",
                  "Include related subdomains within each starting website.",
                ],
                [
                  "open_web",
                  "Follow the wider web",
                  "Follow links to new websites, within your hop and request limits.",
                ],
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
              <summary>Preview boundaries and exclude URLs</summary>
              <Field label="Only include matching URLs">
                <input
                  value={d.allow_pattern}
                  placeholder="Optional regular expression"
                  onChange={(e) => set({ allow_pattern: e.target.value })}
                />
              </Field>
              <Field label="Exclude matching URLs">
                <input
                  value={d.deny_pattern}
                  placeholder="/logout|/calendar/"
                  onChange={(e) => set({ deny_pattern: e.target.value })}
                />
              </Field>
              <Field label="Try a URL against these rules">
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
                    (preview.allowed
                      ? "Included by these boundaries and URL rules."
                      : "Excluded by these boundaries or URL rules.")}{" "}
                  Depth and robots rules are checked during the crawl.
                </p>
              )}
              <label className="crawl-check">
                <input
                  type="checkbox"
                  checked={network.restrict_hosts}
                  onChange={(e) => net({ restrict_hosts: e.target.checked })}
                />
                Restrict destination hostnames
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
                      Allow these hostnames and their subdomains. Applies to
                      pages, feeds, robots files, and redirects.
                    </small>
                  </p>
                </>
              )}
              <div className="crawl-pair">
                <Field label="Levels within a website">
                  <input
                    type="number"
                    min="1"
                    value={d.max_depth}
                    onChange={(e) => set({ max_depth: Number(e.target.value) })}
                  />
                </Field>
                <Field label="Hops between websites">
                  <input
                    type="number"
                    min="1"
                    value={d.max_hops}
                    onChange={(e) => set({ max_hops: Number(e.target.value) })}
                  />
                </Field>
              </div>
            </details>
            <h3>What should it prioritize?</h3>
            <fieldset className="crawl-choices">
              <legend className="sr-only">Traversal strategy</legend>
              {[
                [
                  "host_diverse",
                  "Balanced coverage",
                  "Rotate websites so a large site does not dominate.",
                ],
                [
                  "breadth_first",
                  "Cover the surface",
                  "Visit nearby pages before moving deeper.",
                ],
                [
                  "depth_first",
                  "Follow a trail",
                  "Explore deeper paths before returning to other branches.",
                ],
                [
                  "adaptive",
                  "Learn from results",
                  "Favor sites yielding unique HTML while continuing to explore.",
                ],
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
            <h3>Keep watching, or finish?</h3>
            <fieldset className="crawl-choices">
              <legend className="sr-only">Run behavior</legend>
              <Choice
                group="completion"
                value="monitor"
                selected={d.termination.when_drained ? "drain" : "monitor"}
                title="Keep monitoring"
                onChange={() => terms({ when_drained: false })}
              >
                An empty queue means caught up. Revisit pages on your cadence.
              </Choice>
              <Choice
                group="completion"
                value="drain"
                selected={d.termination.when_drained ? "drain" : "monitor"}
                title="Finish when the queue drains"
                onChange={() => terms({ when_drained: true })}
              >
                Read sources once, explore their links, and finish after
                processing settles.
              </Choice>
            </fieldset>
            {!d.termination.when_drained && (
              <Field label="Revisit pages every">
                <input
                  value={d.revisit}
                  onChange={(e) => set({ revisit: e.target.value })}
                />
              </Field>
            )}
            <details>
              <summary>Set a request or running-time limit</summary>
              <p>
                After the limit, dispatched work finishes and remaining
                discoveries are preserved.
              </p>
              <div className="crawl-pair">
                <Field label="Maximum page fetches">
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
                <Field label="Running-time limit">
                  <input
                    placeholder="No limit, or e.g. 6h"
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
                ["direct", "Direct", "Connect without a proxy."],
                [
                  "http",
                  "HTTP / HTTPS proxy",
                  "Proxy page requests, feeds, robots checks, and redirects.",
                ],
                [
                  "socks5",
                  "SOCKS5 proxy",
                  "Route this crawl through a SOCKS5 server.",
                ],
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
              Require a proxy connection
            </label>
            {network.require_proxy && (
              <p>
                <small>
                  A proxy must be configured. Requests stop if it is
                  unavailable; they never fall back to direct access.
                </small>
              </p>
            )}
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
                  Separate proxy sessions for each run
                </label>
                {network.isolate_proxy_sessions && (
                  <p>
                    <small>
                      Use a proxy that accepts generated SOCKS credentials as
                      session labels, and enter its address without login
                      credentials. Pages, feeds, and robots checks share one
                      label per run. The proxy decides how sessions are
                      separated.
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
              <summary>Request timeouts and retries</summary>
              <div className="crawl-pair">
                <Field label="Request timeout">
                  <input
                    placeholder="10s (default)"
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
                    placeholder="100ms (default)"
                    value={network.retry_delay}
                    onChange={(e) => net({ retry_delay: e.target.value })}
                  />
                </Field>
                <Field label="Maximum retry delay">
                  <input
                    placeholder="5s (default)"
                    value={network.retry_max_delay}
                    onChange={(e) => net({ retry_max_delay: e.target.value })}
                  />
                </Field>
              </div>
              <Field label="User-Agent">
                <input
                  placeholder="Default crawler User-Agent"
                  value={network.user_agent}
                  onChange={(e) => net({ user_agent: e.target.value })}
                />
              </Field>
              <p>
                <small>
                  The timeout covers each network request after pacing waits.
                  Retry delays increase up to your maximum; server Retry-After
                  instructions take precedence.
                </small>
              </p>
            </details>
            <FetchSettings
              value={d.fetch}
              onChange={(fetch) => set({ fetch })}
              onError={onError}
            />
            <details>
              <summary>Request pacing</summary>
              <p>
                All crawls share a per-site request budget. Each crawl can
                choose stricter pacing.
              </p>
              <div className="crawl-pair">
                <Field label="Delay between requests">
                  <input
                    value={d.request_delay}
                    onChange={(e) => set({ request_delay: e.target.value })}
                  />
                </Field>
                <Field label="Parallel requests per site">
                  <input
                    type="number"
                    min="1"
                    max="2"
                    value={d.concurrency}
                    onChange={(e) =>
                      set({ concurrency: Number(e.target.value) })
                    }
                  />
                </Field>
              </div>
            </details>
            <h3>How long should it remember?</h3>
            <p>
              Detailed history can expire while URL identities remain
              remembered. Accepted work is protected.
            </p>
            <Lifetime
              label="Completed page details"
              value={d.retention.completed_for}
              onChange={(v) => retention("completed_for", v)}
            />
            <Lifetime
              label="Failed page details"
              value={d.retention.failed_for}
              onChange={(v) => retention("failed_for", v)}
            />
            <Lifetime
              label="Remember visited URLs"
              value={d.retention.dedup_for}
              onChange={(v) => retention("dedup_for", v)}
            />
            <small>
              Remembered URLs must outlast detailed records. Monitoring revisits
              are independent of retention.
            </small>
            {editor.record && (
              <details>
                <summary>Expert worker pipeline</summary>
                <p>
                  This saves your guided draft, then opens its compiled
                  pipeline. Custom pipelines stay in expert mode after saving.
                </p>
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
                  Save draft and open expert editor
                </button>
              </details>
            )}
          </>
        )}
      </section>
      <aside className="crawl-review crawl-panel">
        <h2>{d.name || "Untitled crawl"}</h2>
        {isExpert ? (
          <p>
            The custom pipeline controls sources, traversal, networking, and
            output destinations.
          </p>
        ) : (
          <>
            {" "}
            <p>
              Start from <strong>{d.seeds.length + d.feeds.length}</strong>{" "}
              sources.
            </p>
            <p>
              {d.scope === "open_web"
                ? `Follow the wider web, up to ${d.max_hops} site hops.`
                : d.scope === "same_origin"
                  ? "Stay on each starting host."
                  : "Explore each starting website and its subdomains."}
            </p>
            <p>
              {strategyTitle(d.strategy)} traversal, up to {d.max_depth} site
              levels.
            </p>
          </>
        )}
        <p>
          {d.termination.when_drained
            ? "Finish when sources and discovered work drain."
            : isExpert
              ? "Keep the custom pipeline running until stopped or a limit is reached."
              : `Keep monitoring; revisit every ${d.revisit}.`}
        </p>
        {d.termination.max_requests > 0 && (
          <p>Stop scheduling after {d.termination.max_requests} requests.</p>
        )}
        {d.termination.duration && (
          <p>Stop after {d.termination.duration} of running time.</p>
        )}
        {!isExpert && (
          <>
            {" "}
            <p>
              {d.proxy?.mode && d.proxy.mode !== "direct"
                ? `${d.proxy.mode.toUpperCase()} proxy applies to this crawl only.`
                : "Direct connection."}
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
          {editor.record
            ? "Save and start replacement run"
            : "Create and start crawl"}
        </button>
        <button type="submit" disabled={locked}>
          {editor.record ? "Save draft" : "Save crawl without starting"}
        </button>
        {editor.record && (
          <small>
            Drafts leave the current run unchanged. Replacement runs preserve
            previous history.
          </small>
        )}
      </aside>
    </form>
  );
}
