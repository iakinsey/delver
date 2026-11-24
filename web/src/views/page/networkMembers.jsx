import React, { useState, useEffect } from "react";
export function NetworkMembers({ call, snapshot, hostId }) {
  const [query, setQuery] = useState(""),
    [offset, setOffset] = useState(0),
    [page, setPage] = useState(null),
    [error, setError] = useState("");
  useEffect(() => {
    setQuery("");
    setOffset(0);
  }, [hostId, snapshot]);
  useEffect(() => {
    let live = true;
    const controller = new AbortController();
    setPage(null);
    call(
      "members",
      { snapshot, host_id: hostId, query, offset },
      controller.signal,
    )
      .then((v) => {
        if (live) {
          setPage(v);
          setError("");
        }
      })
      .catch((e) => {
        if (live) setError(e.message);
      });
    return () => {
      live = false;
      controller.abort();
    };
  }, [call, snapshot, hostId, query, offset]);
  if (error) return <p role="alert">{error}</p>;
  if (!page?.family) return null;
  return (
    <section>
      <h3>Family members ({page.total})</h3>
      <p>Internal edges: {page.internal}</p>
      <label>
        Find member
        <input
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setOffset(0);
          }}
        />
      </label>
      <ul>
        {page.members.map((m) => (
          <li key={m.id}>
            <strong>{m.name}</strong>
            <p>
              {m.visited} visited / {m.known} known · {m.incoming} incoming ·{" "}
              {m.outgoing} outgoing
            </p>
          </li>
        ))}
      </ul>
      <button
        disabled={!offset}
        onClick={() => setOffset((n) => Math.max(0, n - 100))}
      >
        Previous members
      </button>
      <button disabled={!page.more} onClick={() => setOffset((n) => n + 100)}>
        Next members
      </button>
    </section>
  );
}
