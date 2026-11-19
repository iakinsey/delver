import { test, expect } from "./test";
const startFixture = async (...args) =>
  (await import("./globalSetup.mjs")).startFixture(...args);

test.describe.configure({ mode: "serial" });
let fixture;
test.beforeAll(async () => {
  fixture = await startFixture();
});
test.afterAll(async () => {
  if (fixture) await fixture.stop();
});
async function api(request, action, data = {}) {
  const response = await request.post(`${fixture.url}/crawls/${action}`, {
    data,
  });
  const body = await response.json();
  expect(body.success, body.error).toBe(true);
  return body.data;
}
async function settings(request) {
  return (
    await (
      await request.post(fixture.url + "/workspace/settings", { data: {} })
    ).json()
  ).data;
}
async function editor(page) {
  if (!new URL(page.url()).pathname.endsWith("/workspace/configuration"))
    await page
      .getByRole("button", { name: "Workspace configuration", exact: true })
      .click();
  await expect(
    page.getByLabel("Process concurrency", { exact: true }),
  ).toBeVisible();
}
async function create(page, request, name) {
  const info = await (await request.get(fixture.url + "/fixture/info")).json();
  await page.goto(fixture.url);
  await page
    .getByRole("button", { name: "＋ Create crawl", exact: true })
    .click();
  await page.getByLabel("Name", { exact: true }).fill(name);
  await page
    .getByLabel("Seed URLs", { exact: true })
    .fill(info.source + "/site");
  await page.getByLabel("Revisit interval", { exact: true }).fill("1s");
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  await expect(page.locator(".crawl-state")).toHaveText("running");
  return new URL(page.url()).pathname.split("/").at(-1);
}
async function stop(page, id) {
  await page.goto(`${fixture.url}/crawls/${id}`);
  const button = page.getByRole("button", { name: "Stop", exact: true });
  await expect(button).toBeVisible();
  await button.click();
  await expect(page.locator(".crawl-state")).toHaveText("stopped");
}
test("workspace limits update running crawls without changing local configuration", async ({
  page,
  request,
}) => {
  const original = await settings(request);
  let a, b;
  try {
    a = await create(page, request, "shared A");
    b = await create(page, request, "shared B");
    const beforeA = (await api(request, "detail", { crawl_id: a })).definition
      .fetch;
    await editor(page);
    await page.getByLabel("Process concurrency", { exact: true }).fill("1");
    await page.getByLabel("Shared host concurrency", { exact: true }).fill("1");
    await page.getByLabel("Shared host delay", { exact: true }).fill("20ms");
    await page
      .getByRole("button", { name: "Apply limits", exact: true })
      .click();
    await expect
      .poll(async () => (await settings(request)).execution.concurrency)
      .toBe(1);
    for (const id of [a, b])
      await expect
        .poll(
          async () =>
            (await api(request, "detail", { crawl_id: id })).runtime.runtime
              .fetch.process_active,
        )
        .toBeLessThanOrEqual(1);
    expect(
      (await api(request, "detail", { crawl_id: a })).definition.fetch,
    ).toEqual(beforeA);
    await page.goto(`${fixture.url}/crawls/${b}`);
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await expect(
      page.getByText("Effective concurrency ceiling: 1 · Process ceiling: 1", {
        exact: true,
      }),
    ).toBeVisible();
    await page.getByText("Request pacing", { exact: true }).click();
    await expect(
      page.getByText(
        "Effective host concurrency: 1 · Shared ceiling: 1 · Shared delay: 20ms",
        { exact: true },
      ),
    ).toBeVisible();
    await page.reload();
    await editor(page);
    await expect(
      page.getByLabel("Process concurrency", { exact: true }),
    ).toHaveValue("1");
  } finally {
    await page.goto(fixture.url);
    await editor(page);
    await page
      .getByLabel("Process concurrency", { exact: true })
      .fill(String(original.execution.concurrency));
    await page
      .getByLabel("Shared host concurrency", { exact: true })
      .fill(String(original.execution.host_concurrency));
    await page
      .getByLabel("Shared host delay", { exact: true })
      .fill(original.execution.host_delay);
    await page
      .getByRole("button", { name: "Apply limits", exact: true })
      .click();
    await expect
      .poll(async () => (await settings(request)).execution.concurrency)
      .toBe(original.execution.concurrency);
    if (a) await stop(page, a);
    if (b) await stop(page, b);
  }
});
test("two workspace editors retain losing input and report a revision conflict", async ({
  page,
  context,
  request,
}) => {
  const other = await context.newPage(),
    original = await settings(request);
  try {
    await page.goto(fixture.url);
    await other.goto(fixture.url);
    await editor(page);
    await editor(other);
    await page.getByLabel("Process concurrency", { exact: true }).fill("9");
    await page
      .getByRole("button", { name: "Apply limits", exact: true })
      .click();
    await expect
      .poll(async () => (await settings(request)).execution.concurrency)
      .toBe(9);
    await other.getByLabel("Process concurrency", { exact: true }).fill("7");
    await other
      .getByRole("button", { name: "Apply limits", exact: true })
      .click();
    await expect(other.getByRole("alert")).toContainText(
      "workspace revision conflict",
    );
    await expect(
      other.getByLabel("Process concurrency", { exact: true }),
    ).toHaveValue("7");
    expect((await settings(request)).execution.concurrency).toBe(9);
    await other.getByRole("button", { name: "Reload", exact: true }).click();
    await expect(
      other.getByLabel("Process concurrency", { exact: true }),
    ).toHaveValue("9");
  } finally {
    await other.close();
    await page
      .getByLabel("Process concurrency", { exact: true })
      .fill(String(original.execution.concurrency));
    await page
      .getByRole("button", { name: "Apply limits", exact: true })
      .click();
    await expect
      .poll(async () => (await settings(request)).execution.concurrency)
      .toBe(original.execution.concurrency);
  }
});
test("shared limit input types reject invalid values", async ({
  page,
  request,
}) => {
  await page.goto(fixture.url);
  await editor(page);
  const before = await settings(request);
  for (const [label, value] of [
    ["Process concurrency", "0"],
    ["Shared host concurrency", "8193"],
  ]) {
    const input = page.getByLabel(label, { exact: true }),
      previous = await input.inputValue();
    await input.fill(value);
    expect(await input.evaluate((node) => node.checkValidity())).toBe(false);
    await input.fill(previous);
  }
  await page.getByLabel("Shared host delay", { exact: true }).fill("tomorrow");
  await page.getByRole("button", { name: "Apply limits", exact: true }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  expect(await settings(request)).toEqual(before);
  await expect(
    page.getByLabel("Shared host delay", { exact: true }),
  ).toHaveValue("tomorrow");
});
test("double stop and deletion during a pending command do not overlap", async ({
  page,
  request,
}) => {
  const id = await create(page, request, "double stop");
  let requests = 0;
  await page.route("**/crawls/command", async (route) => {
    requests++;
    const response = await route.fetch();
    await new Promise((resolve) => setTimeout(resolve, 500));
    await route.fulfill({ response });
  });
  await page
    .getByRole("button", { name: "Stop", exact: true })
    .dblclick({ force: true });
  await expect(page.locator(".crawl-state")).toHaveText("stopping");
  await expect(
    page.getByRole("button", { name: "Delete", exact: true }),
  ).toBeDisabled();
  await expect(page.locator(".crawl-state")).toHaveText("stopped");
  expect(requests).toBe(1);
  expect(
    Object.values(await api(request, "operations", { crawl_id: id })).filter(
      (op) => op.action === "stop",
    ),
  ).toHaveLength(1);
});
test("paused monitoring displays backend counters and capacity", async ({
  page,
  request,
}) => {
  const id = await create(page, request, "monitor counters");
  await expect
    .poll(
      async () =>
        (await api(request, "detail", { crawl_id: id })).runtime.runtime
          .frontier.terminal_outcomes,
    )
    .toBeGreaterThan(0);
  await page.getByRole("button", { name: "Pause", exact: true }).click();
  await expect(page.locator(".crawl-state")).toHaveText("paused");
  await page.reload();
  await expect(page.locator(".crawl-state")).toHaveText("paused");
  const snapshot = (await api(request, "detail", { crawl_id: id })).runtime
    .runtime;
  const metric = (label) =>
    page
      .getByLabel("Fetch metrics", { exact: true })
      .locator(".crawl-flow > div")
      .filter({ has: page.getByText(label, { exact: true }) })
      .locator("strong");
  await expect(metric("Effective concurrency")).toHaveText(
    String(snapshot.fetch.effective),
  );
  await expect(metric("Process concurrency limit")).toHaveText(
    String(snapshot.fetch.process_limit),
  );
  await expect(
    page
      .getByLabel("Crawl progress", { exact: true })
      .locator("div")
      .filter({ has: page.getByText("Processed", { exact: true }) })
      .locator("strong"),
  ).toHaveText(String(snapshot.frontier.terminal_outcomes));
  await page.getByRole("button", { name: "Stop", exact: true }).click();
  await expect(page.locator(".crawl-state")).toHaveText("stopped");
  const final = (await api(request, "detail", { crawl_id: id })).runs.at(
    -1,
  ).summary;
  expect(final.fetch.active).toBe(0);
  expect(Object.values(final.workers).every((w) => w.active === 0)).toBe(true);
});
test("combined adaptive proxy workflow recovers in the same run and survives application restart", async ({
  page,
  request,
}) => {
  test.setTimeout(90000);
  const token = crypto.randomUUID(),
    agent = `Combined/${token}`,
    info = await (await request.get(fixture.url + "/fixture/info")).json();
  let a, b;
  try {
    await page.goto(fixture.url);
    await page
      .getByRole("button", { name: "＋ Create crawl", exact: true })
      .click();
    await page.getByLabel("Name", { exact: true }).fill("combined adaptive");
    const source = info.source.replace("127.0.0.1", "crawl-proxy.invalid");
    await page
      .getByLabel("Seed URLs", { exact: true })
      .fill(
        [
          source + "/site",
          ...Array.from({ length: 300 }, (_, i) => `${source}/slow?pause=${i}`),
        ].join("\n"),
      );
    await page.getByText("RSS / Atom feeds", { exact: true }).click();
    await page.getByLabel("Feed URLs", { exact: true }).fill(source + "/feed");
    await page.getByRole("radio", { name: "Open web", exact: true }).check();
    await page.getByLabel("Revisit interval", { exact: true }).fill("100ms");
    await page.getByRole("radio", { name: "SOCKS5", exact: true }).check();
    const proxy = new URL(info.socks);
    proxy.username = "";
    proxy.password = "";
    await page.getByLabel("Proxy address", { exact: true }).fill(proxy.href);
    await page.getByLabel("Require proxy", { exact: true }).check();
    await page.getByLabel("Isolate proxy sessions", { exact: true }).check();
    await page.getByLabel("Respect robots.txt", { exact: true }).uncheck();
    await page.getByText("URL filters", { exact: true }).click();
    await page.getByLabel("Restrict hostnames", { exact: true }).check();
    await page
      .getByLabel("Allowed hostname suffixes", { exact: true })
      .fill("crawl-proxy.invalid\n127.0.0.1");
    await page.getByText("Requests", { exact: true }).click();
    await page.getByLabel("User-Agent", { exact: true }).fill(agent);
    await page
      .getByRole("radio", { name: "Adaptive concurrency", exact: true })
      .check();
    for (const [group, fields] of [
      ["Execution", [["Concurrent requests overall", "1"]]],
      ["Resource limits", [["Pressure sampling interval", "20ms"]]],
      ["Adaptive limits", [["Maximum concurrency", "4"]]],
      [
        "Adaptive control",
        [
          ["Adjustment interval", "100ms"],
          ["Concurrency increase step", "1"],
        ],
      ],
    ]) {
      const details = page.locator(".fetch-settings details").filter({
        has: page.locator("summary", { hasText: new RegExp(`^${group}$`) }),
      });
      if (!(await details.evaluate((node) => node.open)))
        await details.locator("summary").click();
      for (const [label, value] of fields)
        await page.getByLabel(label, { exact: true }).fill(value);
    }
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page).toHaveURL(/\/crawls\/[a-f0-9-]+$/);
    a = new URL(page.url()).pathname.split("/").at(-1);
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page.getByText("Worker pipeline", { exact: true }).click();
    await page
      .getByRole("button", { name: "Edit pipeline JSON", exact: true })
      .click();
    const editor = page.getByLabel("Worker pipeline JSON", { exact: true });
    const pipeline = JSON.parse(await editor.inputValue());
    pipeline.resources.push({
      name: "pressure-fixture",
      type: "fixture_pressure",
      parameters: { token },
    });
    await editor.fill(JSON.stringify(pipeline, null, 2));
    await page
      .getByRole("button", { name: "Save and restart", exact: true })
      .click();
    await expect(page.locator(".crawl-state")).toHaveText("running");
    await expect
      .poll(
        async () =>
          (await api(request, "detail", { crawl_id: a })).runtime.runtime.fetch
            .totals.successes,
      )
      .toBeGreaterThan(0);
    const first = (await api(request, "detail", { crawl_id: a })).runs.at(
      -1,
    ).id;
    expect(
      (
        await request.post(fixture.url + "/fixture/pressure", {
          data: { token, fail: true },
        })
      ).ok(),
    ).toBe(true);
    await expect(
      page.getByRole("alert").filter({ hasText: "injected storage pressure" }),
    ).toBeVisible();
    await expect
      .poll(
        async () =>
          (await api(request, "detail", { crawl_id: a })).runtime.runtime.fetch
            .active,
      )
      .toBe(0);
    // Pause with pending durable records while storage admission remains blocked.
    await expect
      .poll(async () => {
        const runtime = (await api(request, "detail", { crawl_id: a })).runtime
          .runtime;
        return (
          Object.values(runtime.queues).reduce((sum, count) => sum + count, 0) +
          (runtime.frontier?.pending_and_active || 0) +
          (runtime.frontier?.deferred_discoveries || 0)
        );
      })
      .toBeGreaterThan(0);
    await page.getByRole("button", { name: "Pause", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("paused", {
      timeout: 15000,
    });
    const blockedPause = await api(request, "detail", { crawl_id: a });
    expect(blockedPause.runtime.runtime.fetch.active).toBe(0);
    expect(blockedPause.runtime.runtime.fetch.process_active).toBe(0);
    expect(
      Object.values(blockedPause.runtime.runtime.queues).reduce(
        (sum, count) => sum + count,
        0,
      ),
    ).toBeGreaterThan(0);
    await page.reload();
    await page.getByRole("button", { name: "Resume", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("running");
    const attempts = (await api(request, "detail", { crawl_id: a })).runtime
      .runtime.fetch.totals.attempts;
    expect(
      (
        await request.post(fixture.url + "/fixture/pressure", {
          data: { token, fail: false },
        })
      ).ok(),
    ).toBe(true);
    await expect
      .poll(
        async () =>
          (await api(request, "detail", { crawl_id: a })).runtime.runtime.fetch
            .totals.attempts,
      )
      .toBeGreaterThan(attempts);
    expect((await api(request, "detail", { crawl_id: a })).runs.at(-1).id).toBe(
      first,
    );
    const events = await (
      await request.get(
        fixture.url +
          "/fixture/request-events?user_agent=" +
          encodeURIComponent(agent),
      )
    ).json();
    expect(events.some((row) => row.path === "/site")).toBe(true);
    expect(events.some((row) => row.path === "/feed")).toBe(true);
    expect(events.some((row) => row.path === "/robots.txt")).toBe(false);
    // An independent fixed crawl drains while the adaptive crawl is paused/resumed.
    await page
      .getByRole("button", { name: "＋ Create crawl", exact: true })
      .click();
    await page.getByLabel("Name", { exact: true }).fill("combined fixed");
    await page
      .getByLabel("Seed URLs", { exact: true })
      .fill(
        Array.from(
          { length: 20 },
          (_, i) => `${info.source}/slow?combined=${i}`,
        ).join("\n"),
      );
    await page.getByRole("radio", { name: /^Drain Read/ }).check();
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    await expect(page).toHaveURL(/\/crawls\/[a-f0-9-]+$/);
    await expect(
      page.getByRole("heading", { name: "combined fixed", exact: true }),
    ).toBeVisible();
    await expect(page).not.toHaveURL(`${fixture.url}/crawls/${a}`);
    b = new URL(page.url()).pathname.split("/").at(-1);
    expect(b).not.toBe(a);
    await page.goto(`${fixture.url}/crawls/${a}`);
    await page.getByRole("button", { name: "Pause", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("paused");
    const paused = (await api(request, "detail", { crawl_id: a })).runs.at(
      -1,
    ).elapsed_ns;
    await page.waitForTimeout(300);
    expect(
      (await api(request, "detail", { crawl_id: a })).runs.at(-1).elapsed_ns,
    ).toBe(paused);
    await page.getByRole("button", { name: "Resume", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("running");
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page.getByLabel("Name", { exact: true }).fill("combined revised");
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    expect((await api(request, "detail", { crawl_id: a })).runs.at(-1).id).toBe(
      first,
    );
    await page
      .getByRole("button", { name: "Apply and restart", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "combined revised", exact: true }),
    ).toBeVisible();
    await expect(page.locator(".crawl-state")).toHaveText("running");
    const before = await api(request, "detail", { crawl_id: a }),
      run = before.runs.at(-1).id;
    expect(before.runs).toHaveLength(1);
    expect(run).toBe(first);
    expect(
      before.runs[0].configuration_snapshot.pipeline.resources.some(
        (r) => r.type === "fixture_pressure",
      ),
    ).toBe(true);
    await fixture.restart();
    await page.reload();
    await expect(page.locator(".crawl-state")).toHaveText("running");
    const restored = await api(request, "detail", { crawl_id: a });
    expect(restored.runs.at(-1).id).toBe(run);
    expect(restored.runs.at(-1).definition.network.user_agent).toBe(agent);
    await expect
      .poll(
        async () =>
          (await api(request, "detail", { crawl_id: b })).runs.at(-1).state,
        { timeout: 20000 },
      )
      .toBe("completed");
    await stop(page, a);
    const final = await api(request, "detail", { crawl_id: a });
    expect(final.runs.at(-1).summary.fetch.active).toBe(0);
    expect(final.runs.at(-1).summary.fetch.process_active).toBe(0);
    for (const [id, preserve] of [
      [a, false],
      [b, true],
    ]) {
      await page.goto(`${fixture.url}/crawls/${id}`);
      await page.getByRole("button", { name: "Delete", exact: true }).click();
      const dialog = page.getByRole("alertdialog");
      await dialog
        .getByRole("checkbox", { name: "Preserve crawl logs", exact: true })
        .setChecked(preserve);
      await dialog.getByRole("button", { name: "Delete", exact: true }).click();
      await expect(page).toHaveURL(fixture.url + "/");
    }
  } finally {
    for (const id of [a, b])
      if (id) {
        const records = await api(request, "list");
        if (records.some((r) => r.id === id)) {
          const d = await api(request, "detail", { crawl_id: id });
          if (test.info().status !== test.info().expectedStatus)
            await test.info().attach("remaining-crawl-state", {
              body: JSON.stringify(d, null, 2),
              contentType: "application/json",
            });
          if (["running", "paused", "draining"].includes(d.runtime?.state)) {
            await page.goto(`${fixture.url}/crawls/${id}`);
            await page
              .getByRole("button", { name: "Stop", exact: true })
              .click();
            await expect(page.locator(".crawl-state")).toHaveText("stopped");
          }
        }
      }
  }
});

test("lowered shared host limits apply to queued requests across crawls", async ({
  page,
  request,
}) => {
  const original = await settings(request),
    ids = [];
  const info = await (await request.get(fixture.url + "/fixture/info")).json();
  const network = async (method = "GET") =>
    (
      await request.fetch(fixture.url + "/fixture/network-activity", { method })
    ).json();
  const active = async () => (await network()).active;
  try {
    for (const name of ["host ceiling A", "host ceiling B"]) {
      await page.goto(fixture.url);
      await page
        .getByRole("button", { name: "＋ Create crawl", exact: true })
        .click();
      await page.getByLabel("Name", { exact: true }).fill(name);
      await page
        .getByLabel("Seed URLs", { exact: true })
        .fill(
          Array.from(
            { length: 20 },
            (_, i) => `${info.source}/slow?${name.replaceAll(" ", "")}-${i}`,
          ).join("\n"),
        );
      await page.getByLabel("Revisit interval", { exact: true }).fill("100ms");
      await page.getByLabel("Respect robots.txt", { exact: true }).uncheck();
      await page
        .getByRole("button", { name: "Create and start", exact: true })
        .click();
      await expect(page).toHaveURL(/\/crawls\/[a-f0-9-]+$/);
      await expect(page.locator(".crawl-state")).toHaveText("running");
      ids.push(new URL(page.url()).pathname.split("/").at(-1));
    }
    await expect.poll(active).toBeGreaterThanOrEqual(2);
    await editor(page);
    await page.getByLabel("Shared host concurrency", { exact: true }).fill("1");
    await page
      .getByRole("button", { name: "Apply limits", exact: true })
      .click();
    await expect
      .poll(async () => (await settings(request)).execution.host_concurrency)
      .toBe(1);
    await expect.poll(active).toBeLessThanOrEqual(1);
    await network("POST");
    await page.waitForTimeout(1200);
    expect((await network()).peak).toBeLessThanOrEqual(1);
  } finally {
    for (const id of ids) await stop(page, id);
    await page.goto(fixture.url);
    await editor(page);
    await page
      .getByLabel("Shared host concurrency", { exact: true })
      .fill(String(original.execution.host_concurrency));
    await page
      .getByRole("button", { name: "Apply limits", exact: true })
      .click();
    await expect
      .poll(async () => (await settings(request)).execution.host_concurrency)
      .toBe(original.execution.host_concurrency);
  }
});
