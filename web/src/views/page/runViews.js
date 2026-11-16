import React from "react";
import { words } from "./crawlApi";
export function ResultsPanel({ results, resultsRun, setResultsRun }) {
  return (
    <section className="crawl-panel">
      <h2>Recent results</h2>
      {resultsRun && (
        <p>
          Run: {resultsRun}{" "}
          <button onClick={() => setResultsRun("")}>Latest</button>
        </p>
      )}
      {results.available === false ? (
        <p>Custom output sink</p>
      ) : results.rows?.length ? (
        <div className="crawl-table-scroll">
          <table>
            <thead>
              <tr>
                <th>Page</th>
                <th>HTTP status</th>
                <th>Title or error</th>
              </tr>
            </thead>
            <tbody>
              {results.rows.map((row, i) => (
                <tr key={`${row.uri}:${i}`}>
                  <td className="crawl-result-uri">
                    {/^https?:\/\//.test(row.uri) ? (
                      <a href={row.uri} target="_blank" rel="noreferrer">
                        {row.uri}
                      </a>
                    ) : (
                      row.uri
                    )}
                  </td>
                  <td>{row.http_status || "Unavailable"}</td>
                  <td>{row.error || row.title || "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p>Empty</p>
      )}
      {results.limited && <small>Partial results</small>}
    </section>
  );
}
export function RunHistory({ runs, onView }) {
  return (
    <section className="crawl-panel">
      <h2>Run history</h2>
      <ul>
        {[...runs].reverse().map((r) => (
          <li key={r.id}>
            {new Date(r.created * 1000).toLocaleString()} · version {r.version}{" "}
            · {words(r.state)}
            {r.continued > 0 ? ` · continued ${r.continued} time(s)` : ""}{" "}
            <button onClick={() => onView(r.id)}>View</button>
            <details>
              <summary>Configuration</summary>
              <pre>
                {JSON.stringify(
                  r.configuration_snapshot || {
                    definition: r.definition,
                    pipeline: r.expert,
                  },
                  null,
                  2,
                )}
              </pre>
              <p>
                {r.expert
                  ? "Custom pipeline"
                  : `${words(r.definition.strategy)}; ${words(r.definition.scope)}; ${r.definition.termination.when_drained ? "drain" : "revisit interval: " + r.definition.revisit}`}
              </p>
            </details>
          </li>
        ))}
      </ul>
    </section>
  );
}
