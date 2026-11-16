test("caller cancellation reaches fetch and the request resolves with feedback", async () => {
  const { request } = await import("./util");
  const original = globalThis.fetch;
  globalThis.fetch = (url, { signal }) =>
    new Promise((resolve, reject) =>
      signal.addEventListener("abort", () => reject(Error("cancelled"))),
    );
  try {
    const controller = new AbortController();
    const pending = request("crawls/list", {}, { signal: controller.signal });
    controller.abort();
    expect((await pending).success).toBe(false);
  } finally {
    globalThis.fetch = original;
    localStorage.clear();
  }
});
