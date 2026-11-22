import React from "react";
import { useHistory, useLocation } from "react-router-dom";

export default function CrawlNavigation({ crawlId, active }) {
  const history = useHistory();
  const location = useLocation();
  const runId = new URLSearchParams(location.search).get("run_id");
  const query = runId ? `?run_id=${encodeURIComponent(runId)}` : "";
  const root = `/crawls/${encodeURIComponent(crawlId)}`;
  return (
    <nav className="crawl-actions" aria-label="Crawl views">
      {[
        ["Crawl", root],
        ["Reports", `${root}/report${query}`],
        ["Graph", `${root}/network${query}`],
      ].map(([label, path]) => (
        <button
          key={label}
          aria-current={active === label ? "page" : undefined}
          onClick={() => history.push(path)}
        >
          {label}
        </button>
      ))}
    </nav>
  );
}
