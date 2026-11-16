import React from "react";
const number = (value) =>
  Number(value || 0).toLocaleString(undefined, { maximumFractionDigits: 2 });
export function FetchMetrics({ data, frontier }) {
  if (!data) return null;
  return (
    <section className="crawl-panel" aria-label="Fetch performance">
      <h2>Fetch performance</h2>
      <div className="crawl-flow">
        {[
          ["Attempts / second", data.rates?.attempts],
          ["Successful responses / second", data.rates?.successes],
          ["HTML responses / second", data.rates?.html],
          ["Unique HTML / second", data.rates?.unique_html],
          ["Discovered URLs / second", data.rates?.admitted],
          ["Finalized requests / second", data.rates?.terminal_outcomes],
          ["Bytes / second", data.rates?.bytes],
          ["Active requests", data.active],
          ["Allowed requests", data.effective],
          ["Process active requests", data.process_active],
          ["Process request ceiling", data.process_limit],
          ["Scheduled retries", frontier?.retries_scheduled],
          ["Waiting retries", frontier?.retry_pending],
          ["Host cooldown events", frontier?.host_cooldowns],
        ].map(([label, value]) => (
          <div key={label}>
            <strong>{number(value)}</strong>
            <span>{label}</span>
          </div>
        ))}
      </div>
      <p role="status">
        {data.reason}
        {data.last_adjustment ? ` · ${data.last_adjustment}` : ""}
      </p>
      <details>
        <summary>Network and execution metrics</summary>
        <div
          className="crawl-table-scroll"
          tabIndex={0}
          role="region"
          aria-label="Fetch phase latency measurements"
        >
          <table>
            <thead>
              <tr>
                <th>Phase</th>
                <th>Samples</th>
                <th>Mean ms</th>
                <th>p50 ms</th>
                <th>p95 ms</th>
                <th>Max ms</th>
              </tr>
            </thead>
            <tbody>
              {Object.entries(data.latencies || {}).map(([key, v]) => (
                <tr key={key}>
                  <td>{key.replaceAll("_", " ")}</td>
                  <td>{number(v.count)}</td>
                  <td>{number(v.mean_ms)}</td>
                  <td>{number(v.p50_ms)}</td>
                  <td>{number(v.p95_ms)}</td>
                  <td>{number(v.max_ms)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p>
          Received: {number(data.totals?.bytes_received)} bytes · Stored:{" "}
          {number(data.totals?.bytes_stored)} bytes
        </p>
        <p>
          Memory: {number(data.pressure?.memory_mb)} MiB · CPU:{" "}
          {number(data.pressure?.cpu_percent)}% · Free disk:{" "}
          {number(data.pressure?.disk_free_mb)} MiB
        </p>
        <ul>
          {Object.entries(data.errors || {}).map(([key, value]) => (
            <li key={key}>
              {key}: {number(value)}
            </li>
          ))}
        </ul>
        <ul>
          {Object.entries(data.http_statuses || {}).map(([key, value]) => (
            <li key={key}>
              HTTP {key}: {number(value)}
            </li>
          ))}
        </ul>
        <ul>
          {Object.entries(data.counters || {}).map(([key, value]) => (
            <li key={key}>
              {key}: {number(value)}
            </li>
          ))}
        </ul>
        <ul>
          {Object.entries(data.pressure?.queues || {}).map(([key, value]) => (
            <li key={key}>
              {key}: {number(value)} pending/active · oldest{" "}
              {number(data.pressure?.oldest_seconds?.[key])}s · growth{" "}
              {number(data.queue_growth_per_second?.[key])}/s
            </li>
          ))}
        </ul>
      </details>
    </section>
  );
}
