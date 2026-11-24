import React, { useState, useEffect } from "react";
import { crawlAPI } from "./crawlApi";
export function RepeatedFamilies({
  crawlId,
  runId = "",
  configuration = false,
  revision,
}) {
  const [open, setOpen] = useState(false),
    [query, setQuery] = useState(""),
    [after, setAfter] = useState(""),
    [view, setView] = useState(null),
    [members, setMembers] = useState(null),
    [family, setFamily] = useState(""),
    [memberQuery, setMemberQuery] = useState(""),
    [memberCursors, setMemberCursors] = useState([""]),
    [error, setError] = useState(""),
    [loadError, setLoadError] = useState(""),
    [busy, setBusy] = useState(false),
    [operations, setOperations] = useState({}),
    [refresh, setRefresh] = useState(0);
  const data = { crawl_id: crawlId, run_id: runId };
  useEffect(() => {
    if (!open) return;
    let live = true;
    const controller = new AbortController();
    let timer;
    async function poll() {
      try {
        const [v, ops] = await Promise.all([
          crawlAPI("repeated_families", { ...data, query, after, limit: 20 }),
          crawlAPI("operations", data),
        ]);
        if (live) {
          setView(v);
          setOperations(ops);
          setLoadError("");
        }
      } catch (e) {
        if (live) setLoadError(e.message);
      } finally {
        if (live) timer = setTimeout(poll, 3000);
      }
    }
    poll();
    return () => {
      live = false;
      controller.abort();
      clearTimeout(timer);
    };
  }, [open, crawlId, runId, query, after, refresh]);
  useEffect(() => setMemberCursors([""]), [family, memberQuery]);
  useEffect(() => {
    if (!family) {
      setMembers(null);
      return;
    }
    let live = true;
    crawlAPI("repeated_members", {
      ...data,
      family,
      query: memberQuery,
      after: memberCursors[memberCursors.length - 1],
      limit: 50,
    })
      .then((v) => {
        if (live) setMembers(v);
      })
      .catch((e) => {
        if (live) setError(e.message);
      });
    return () => {
      live = false;
    };
  }, [family, memberQuery, memberCursors, crawlId, runId, refresh]);
  async function command(action, extra = {}) {
    setBusy(true);
    setError("");
    try {
      await crawlAPI(action, {
        ...data,
        id: crypto.randomUUID(),
        revision,
        ...extra,
      });
      setRefresh((n) => n + 1);
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }
  const jobs = Object.values(operations).filter((op) =>
    op.action?.startsWith("repeated_"),
  );
  return (
    <details
      className="crawl-panel"
      onToggle={(e) => setOpen(e.currentTarget.open)}
    >
      <summary>Repeated-page families</summary>
      {open && (
        <>
          <label>
            Find family or site
            <input
              aria-label="Find family or site"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setAfter("");
              }}
            />
          </label>
          {(error || loadError) && <p role="alert">{error || loadError}</p>}
          {view && (
            <>
              <p>
                Mode: {view.mode} · Routing: {view.routing.phase || "Ready"} ·{" "}
                {view.routing.completed}/{view.routing.target}
              </p>
              {configuration && view.mode === "off" && (
                <p>
                  Legacy index restoration:{" "}
                  {view.downgrade_ready ? "Completed" : "Pending"}
                </p>
              )}
              <dl>
                {Object.entries(view.coverage).map(([k, v]) => (
                  <React.Fragment key={k}>
                    <dt>{k.replaceAll("_", " ")}</dt>
                    <dd>{v}</dd>
                  </React.Fragment>
                ))}
              </dl>
              <ul>
                {view.families.map((f) => (
                  <li key={f.id}>
                    <button
                      type="button"
                      onClick={() => {
                        setFamily(f.id);
                        setMemberQuery("");
                      }}
                    >
                      {f.id.slice(0, 12)}
                    </button>{" "}
                    ·{" "}
                    {f.confirmed
                      ? f.exempt
                        ? "Exempt"
                        : "Confirmed"
                      : "Provisional"}{" "}
                    · {f.sites} sites · {f.observations} observations
                    <p>
                      Structure {f.measurements.structure_similarity.toFixed(3)}{" "}
                      · Text {f.measurements.text_similarity.toFixed(3)} ·
                      Destinations{" "}
                      {f.measurements.destination_similarity.toFixed(3)}
                    </p>
                    {configuration && (
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() =>
                          command("repeated_override", {
                            family: f.id,
                            exempt: !f.exempt,
                          })
                        }
                      >
                        {f.exempt ? "Re-enable" : "Exempt"}
                      </button>
                    )}
                  </li>
                ))}
              </ul>
              {after && (
                <button type="button" onClick={() => setAfter("")}>
                  First families
                </button>
              )}
              {view.after && (
                <button type="button" onClick={() => setAfter(view.after)}>
                  Next families
                </button>
              )}
            </>
          )}
          {family && (
            <section>
              <h3>Family members</h3>
              <label>
                Find family member
                <input
                  value={memberQuery}
                  onChange={(e) => setMemberQuery(e.target.value)}
                />
              </label>
              <ul>
                {members?.hosts.map((h) => (
                  <li key={h.site}>
                    {h.site} · {h.family_observations} family / {h.eligible}{" "}
                    eligible / {h.samples} retained observations ·{" "}
                    {h.family === family ? "Dominant" : "Mixed"}
                    {h.reason ? ` · ${h.reason.replaceAll("_", " ")}` : ""}
                  </li>
                ))}
              </ul>
              {memberCursors.length > 1 && (
                <button
                  type="button"
                  onClick={() => setMemberCursors((v) => v.slice(0, -1))}
                >
                  Previous members
                </button>
              )}
              {members?.after && (
                <button
                  type="button"
                  onClick={() => setMemberCursors((v) => [...v, members.after])}
                >
                  Next members
                </button>
              )}
            </section>
          )}
          {configuration && (
            <button
              type="button"
              disabled={
                busy ||
                jobs.some(
                  (o) => o.state === "running" || o.state === "accepted",
                )
              }
              onClick={() => command("repeated_analyze")}
            >
              Analyze stored results
            </button>
          )}
          {jobs.map((job) => (
            <div key={job.id}>
              <p>
                {job.action.replaceAll("_", " ")}: {job.state}
                {job.error ? ` · ${job.error}` : ""}
              </p>
              {configuration &&
                job.action === "repeated_override" &&
                job.state === "failed" &&
                job.input && (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() =>
                      command("repeated_override", {
                        family: job.input.family,
                        exempt: job.input.exempt,
                        run_id: job.input.run_id,
                      })
                    }
                  >
                    Retry reclassification
                  </button>
                )}
              {job.analysis && (
                <p>
                  {job.analysis.records} analyzed · {job.analysis.offset}/
                  {job.analysis.limit} bytes · {job.analysis.oversized}{" "}
                  oversized · {job.analysis.unavailable} unavailable ·{" "}
                  {job.analysis.protected} protected
                </p>
              )}
              {configuration &&
                job.action === "repeated_analyze" &&
                (job.state === "running" || job.state === "accepted" ? (
                  <button
                    type="button"
                    onClick={() =>
                      command("repeated_cancel", { operation_id: job.id })
                    }
                  >
                    Cancel analysis
                  </button>
                ) : (
                  job.analysis &&
                  !job.analysis.complete && (
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() =>
                        command("repeated_analyze", { resume: job.id })
                      }
                    >
                      Resume analysis
                    </button>
                  )
                ))}
            </div>
          ))}
        </>
      )}
    </details>
  );
}
