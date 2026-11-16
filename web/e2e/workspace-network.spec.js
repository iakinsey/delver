import { test, expect } from "./test";
import {
  base,
  info,
  api,
  createForm,
  savedRecord,
  progress,
  cleanup,
} from "./helpers/workspace";
test.describe.configure({ mode: "parallel" });
const basic = {
  strategy: "host_diverse",
  scope: "open_web",
  source: "both",
  connection: "socks5",
  drain: true,
};
async function networkForm(
  page,
  request,
  {
    required = true,
    restricted = true,
    isolate = true,
    source = "both",
    drain = true,
    path = "private",
    revisit = "2s",
  } = {},
) {
  const fixture = await info(request),
    host = `network-${crypto.randomUUID().replaceAll("-", "")}.onion`;
  const root = new URL(fixture.source);
  root.hostname = host;
  await createForm(page, fixture, {
    ...basic,
    name: `network ${host}`,
    source,
    drain,
    revisit,
  });
  if (source !== "feeds")
    await page
      .getByLabel("Seed URLs", { exact: true })
      .fill(`${root.origin}/${path}`);
  if (source !== "urls")
    await page
      .getByLabel("Feed URLs", { exact: true })
      .fill(`${root.origin}/private-feed`);
  if (required)
    await page
      .getByRole("checkbox", {
        name: "Require proxy",
        exact: true,
      })
      .check();
  if (isolate) {
    const proxy = new URL(fixture.socks);
    proxy.username = "";
    proxy.password = "";
    await page
      .getByLabel("Proxy address", { exact: true })
      .fill(proxy.href.replace("socks5:", "socks5h:"));
    await page
      .getByRole("checkbox", {
        name: "Isolate proxy sessions",
        exact: true,
      })
      .check();
  }
  if (restricted) {
    await page.getByText("URL filters", { exact: true }).click();
    await page
      .getByRole("checkbox", {
        name: "Restrict hostnames",
        exact: true,
      })
      .check();
    await page
      .getByLabel("Allowed hostname suffixes", { exact: true })
      .fill(".ONION");
  }
  await page.getByText("Requests", { exact: true }).click();
  await page.getByLabel("Request timeout", { exact: true }).fill("120s");
  await page.getByLabel("Retries per request", { exact: true }).fill("0");
  await page.getByLabel("Initial retry delay", { exact: true }).fill("1s");
  await page.getByLabel("Maximum retry delay", { exact: true }).fill("30s");
  const agent = `NetworkTest/${crypto.randomUUID()}`;
  await page.getByLabel("User-Agent", { exact: true }).fill(agent);
  return { fixture, host, root, agent };
}
async function events(request, path) {
  return (await request.get(`${base}/fixture/${path}`)).json();
}
for (const required of [false, true])
  for (const restricted of [false, true])
    for (const isolate of [false, true])
      test(`generic networking require=${required} restrict=${restricted} sessions=${isolate}`, async ({
        page,
        request,
      }) => {
        test.setTimeout(45000);
        const { host, root, agent } = await networkForm(page, request, {
          required,
          restricted,
          isolate,
        });
        await page
          .getByRole("button", { name: "Create and start", exact: true })
          .click();
        const record = await savedRecord(page, request);
        await expect(page.locator(".crawl-state")).toHaveText("completed", {
          timeout: 20000,
        });
        const { rows } = await api(request, "results", { crawl_id: record.id });
        expect(
          rows.some(
            (r) =>
              new URL(r.uri).pathname === "/private-page" &&
              r.title === agent &&
              r.http_status === 200,
          ),
        ).toBe(true);
        expect(rows.some((r) => new URL(r.uri).pathname === "/outside")).toBe(
          !restricted,
        );
        if (restricted) {
          expect(
            rows.every((r) => new URL(r.uri).hostname.endsWith(".onion")),
          ).toBe(true);
          expect(
            rows.find((r) => new URL(r.uri).pathname === "/private-redirect")
              .error,
          ).toContain("allowed suffixes");
        }
        const traces = (await events(request, "socks-events")).filter(
          (e) => e.host === host,
        );
        expect(traces.length).toBeGreaterThan(0);
        expect(traces.every((e) => e.address_type === 3)).toBe(true);
        const labels = new Set(traces.map((e) => e.user));
        expect(labels.size).toBe(1);
        if (isolate) {
          expect([...labels][0]).toMatch(/^session-/);
          expect(
            JSON.stringify(
              await api(request, "detail", { crawl_id: record.id }),
            ),
          ).not.toContain([...labels][0]);
        }
        const requests = await events(request, "request-events");
        expect(
          requests
            .filter((e) => e.host === root.host)
            .every((e) => e.user_agent === agent),
        ).toBe(true);
        if (restricted)
          expect(
            requests.filter(
              (e) => new URLSearchParams(e.query).get("from") === root.host,
            ),
          ).toHaveLength(0);
        await page.getByRole("button", { name: "Edit", exact: true }).click();
        await expect(
          page.getByLabel("Request timeout", { exact: true }),
        ).toHaveValue("120s");
        await expect(
          page.getByRole("checkbox", {
            name: "Require proxy",
            exact: true,
          }),
        ).toBeChecked({ checked: required });
        await expect(
          page.getByRole("checkbox", {
            name: "Isolate proxy sessions",
            exact: true,
          }),
        ).toBeChecked({ checked: isolate });
      });
test("SOCKS labels survive resume and rotate for a new run", async ({
  page,
  request,
}) => {
  test.setTimeout(60000);
  const { host } = await networkForm(page, request, {
    source: "urls",
    drain: false,
    path: "private-page",
    revisit: "300ms",
  });
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  const record = await savedRecord(page, request);
  try {
    await expect
      .poll(() => progress(page, "Processed"), { timeout: 15000 })
      .toBeGreaterThan(0);
    const labelsBefore = new Set(
      (await events(request, "socks-events"))
        .filter((e) => e.host === host)
        .map((e) => e.user),
    );
    expect(labelsBefore.size).toBe(1);
    const run = (await api(request, "detail", { crawl_id: record.id })).runs.at(
      -1,
    ).id;
    await page.getByRole("button", { name: "Stop", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("stopped", {
      timeout: 15000,
    });
    await page.getByRole("button", { name: "Resume", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("running", {
      timeout: 15000,
    });
    await expect
      .poll(() => progress(page, "Processed"), { timeout: 15000 })
      .toBeGreaterThan(1);
    expect(
      new Set(
        (await events(request, "socks-events"))
          .filter((e) => e.host === host)
          .map((e) => e.user),
      ),
    ).toEqual(labelsBefore);
    await page.getByRole("button", { name: "Restart", exact: true }).click();
    await expect
      .poll(
        async () =>
          (await api(request, "detail", { crawl_id: record.id })).runs.at(-1)
            .id,
        { timeout: 15000 },
      )
      .not.toBe(run);
    await expect
      .poll(
        async () =>
          new Set(
            (await events(request, "socks-events"))
              .filter((e) => e.host === host)
              .map((e) => e.user),
          ).size,
        { timeout: 15000 },
      )
      .toBe(2);
  } finally {
    await cleanup(request, record);
  }
});
test("hostname restrictions also block robots redirects", async ({
  page,
  request,
}) => {
  const { fixture, host, root } = await networkForm(page, request, {
    source: "urls",
    path: "private-page",
  });
  const target = new URL(root.origin);
  target.hostname = `robots-escape.${host}`;
  await page
    .getByLabel("Seed URLs", { exact: true })
    .fill(`${target.origin}/private-page`);
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  const record = await savedRecord(page, request);
  await expect(page.locator(".crawl-state")).toHaveText("completed", {
    timeout: 20000,
  });
  const { rows } = await api(request, "results", { crawl_id: record.id });
  expect(rows).toHaveLength(1);
  expect(rows[0].error).toContain("allowed suffixes");
  expect(
    (await events(request, "request-events")).filter(
      (e) => new URLSearchParams(e.query).get("from") === target.host,
    ),
  ).toHaveLength(0);
});
const invalid = [
  ["Request timeout", "0s"],
  ["Request timeout", "tomorrow"],
  ["Retries per request", "-1"],
  ["Retries per request", "11"],
  ["Retries per request", "1.5"],
  ["Initial retry delay", "0s"],
  ["Initial retry delay", "-1s"],
  ["Maximum retry delay", "1ms"],
];
for (const [field, value] of invalid)
  test(`network validation ${field} ${value}`, async ({ page, request }) => {
    await createForm(page, await info(request), {
      ...basic,
      connection: "direct",
      source: "urls",
      name: `invalid ${field}`,
    });
    await page.getByText("Requests", { exact: true }).click();
    await page.getByLabel(field, { exact: true }).fill(value);
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    await expect(page.getByRole("alert")).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Create crawl", exact: true }),
    ).toBeVisible();
  });
test("required proxy and restricted host validation cannot silently fall back", async ({
  page,
  request,
}) => {
  await createForm(page, await info(request), {
    ...basic,
    connection: "direct",
    source: "urls",
    name: "proxy guard",
  });
  await page
    .getByRole("checkbox", { name: "Require proxy", exact: true })
    .check();
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("proxy required");
  await page
    .getByRole("checkbox", { name: "Require proxy", exact: true })
    .uncheck();
  await page.getByText("URL filters", { exact: true }).click();
  await page
    .getByRole("checkbox", {
      name: "Restrict hostnames",
      exact: true,
    })
    .check();
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "at least one allowed hostname suffix",
  );
  await page
    .getByLabel("Allowed hostname suffixes", { exact: true })
    .fill("onion");
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "must match allowed hostname suffixes",
  );
});
test("automatic session labels do not overwrite proxy authentication", async ({
  page,
  request,
}) => {
  await createForm(page, await info(request), {
    ...basic,
    source: "urls",
    name: "authenticated proxy labels",
  });
  await page
    .getByRole("checkbox", {
      name: "Isolate proxy sessions",
      exact: true,
    })
    .check();
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "without authentication credentials",
  );
});

test("request timeout changes take effect for slow destinations", async ({
  page,
  request,
}) => {
  test.setTimeout(45000);
  const fixture = await info(request);
  await createForm(page, fixture, {
    ...basic,
    source: "urls",
    connection: "direct",
    name: "slow destination",
    path: "slow",
  });
  await page.getByText("Requests", { exact: true }).click();
  await page.getByLabel("Request timeout", { exact: true }).fill("50ms");
  await page.getByLabel("Retries per request", { exact: true }).fill("0");
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  const record = await savedRecord(page, request);
  await expect(page.locator(".crawl-state")).toHaveText("completed", {
    timeout: 15000,
  });
  expect(
    (await api(request, "results", { crawl_id: record.id })).rows[0].error,
  ).toMatch(/deadline|timeout/i);
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await page.getByText("Requests", { exact: true }).click();
  await page.getByLabel("Request timeout", { exact: true }).fill("2s");
  await page
    .getByRole("button", {
      name: "Save and restart",
      exact: true,
    })
    .click();
  await expect(
    page.getByRole("button", { name: "Edit", exact: true }),
  ).toBeEnabled({ timeout: 15000 });
  await expect(page.locator(".crawl-state")).toHaveText("completed", {
    timeout: 15000,
  });
  expect(
    (await api(request, "results", { crawl_id: record.id })).rows[0]
      .http_status,
  ).toBe(200);
});
for (const retries of [0, 2])
  test(`retry count and backoff execute with ${retries} retries`, async ({
    page,
    request,
  }) => {
    const fixture = await info(request),
      query = `case=${crypto.randomUUID()}`;
    await createForm(page, fixture, {
      ...basic,
      source: "urls",
      connection: "direct",
      name: `retry ${query}`,
      path: `retry?${query}`,
    });
    await page.getByText("Requests", { exact: true }).click();
    await page
      .getByLabel("Retries per request", { exact: true })
      .fill(String(retries));
    await page.getByLabel("Initial retry delay", { exact: true }).fill("250ms");
    await page.getByLabel("Maximum retry delay", { exact: true }).fill("500ms");
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    const record = await savedRecord(page, request);
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 15000,
    });
    const attempts = (await events(request, "request-events")).filter(
      (e) => e.path === "/retry" && e.query === query,
    );
    expect(attempts).toHaveLength(retries + 1);
    expect(
      (await api(request, "results", { crawl_id: record.id })).rows[0]
        .http_status,
    ).toBe(retries === 0 ? 503 : 200);
    if (retries === 2) {
      expect(
        Number(attempts[1].time_ms) - Number(attempts[0].time_ms),
      ).toBeGreaterThanOrEqual(240);
      expect(
        Number(attempts[2].time_ms) - Number(attempts[1].time_ms),
      ).toBeGreaterThanOrEqual(490);
    }
  });
test("stopping cancels a long-timeout connection without waiting for its deadline", async ({
  page,
  request,
}) => {
  const fixture = await info(request);
  await createForm(page, fixture, {
    ...basic,
    source: "urls",
    connection: "direct",
    name: "cancel long timeout",
    path: "stall",
  });
  await page.getByText("Requests", { exact: true }).click();
  await page.getByLabel("Request timeout", { exact: true }).fill("120s");
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  const record = await savedRecord(page, request);
  try {
    await expect
      .poll(
        async () =>
          (await events(request, "request-events")).some(
            (e) => e.path === "/stall",
          ),
        { timeout: 15000 },
      )
      .toBe(true);
    await page.getByRole("button", { name: "Stop", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("stopped", {
      timeout: 10000,
    });
  } finally {
    await cleanup(request, record);
  }
});
