import { test, expect } from "./test";
import {
  base,
  api,
  info,
  createForm,
  savedRecord,
  cleanup,
} from "./helpers/workspace";
test.describe.configure({ mode: "parallel" });
const basic = {
  strategy: "breadth_first",
  scope: "same_origin",
  source: "urls",
  connection: "direct",
  drain: false,
  revisit: "24h",
};
for (const state of ["paused", "stopped"])
  for (const save of ["resume", "restart"])
    test(`configuration ${state} save-and-${save} retains run and pending work`, async ({
      page,
      request,
    }) => {
      const fixture = await info(request),
        token = crypto.randomUUID();
      await createForm(page, fixture, {
        ...basic,
        name: `state ${state} ${save}`,
      });
      await page
        .getByLabel("Seed URLs", { exact: true })
        .fill(
          Array.from(
            { length: 60 },
            (_, i) => `${fixture.source}/slow?token=${token}&page=${i}`,
          ).join("\n"),
        );
      await page.getByText("Requests", { exact: true }).click();
      await page
        .getByLabel("User-Agent", { exact: true })
        .fill(`initial-${token}`);
      await page.getByText("Request pacing", { exact: true }).click();
      await page.getByLabel("Per-host concurrency", { exact: true }).fill("1");
      await page
        .getByRole("radio", { name: "Fixed concurrency", exact: true })
        .check();
      const execution = page.locator(".fetch-settings details").filter({
        has: page.locator("summary", { hasText: /^Execution$/ }),
      });
      if (!(await execution.evaluate((node) => node.open)))
        await execution.locator("summary").click();
      await page
        .getByLabel("Concurrent requests overall", { exact: true })
        .fill("1");
      await page
        .getByRole("button", { name: "Create and start", exact: true })
        .click();
      const record = await savedRecord(page, request);
      try {
        await expect
          .poll(
            async () =>
              (await api(request, "detail", { crawl_id: record.id })).runtime
                ?.runtime?.frontier?.pending_and_active || 0,
          )
          .toBeGreaterThan(20);
        await page
          .getByRole("button", {
            name: state === "paused" ? "Pause" : "Stop",
            exact: true,
          })
          .click();
        await expect(page.locator(".crawl-state")).toHaveText(state);
        const before = await api(request, "detail", { crawl_id: record.id }),
          run = before.runs.at(-1);
        await page.getByRole("button", { name: "Edit", exact: true }).click();
        await page.getByText("Request pacing", { exact: true }).click();
        await page
          .getByLabel("Per-host concurrency", { exact: true })
          .fill("4");
        await page.getByText("Requests", { exact: true }).click();
        await page
          .getByLabel("User-Agent", { exact: true })
          .fill(`updated-${token}`);
        await page
          .getByRole("button", {
            name: save === "resume" ? "Save draft" : "Save and restart",
            exact: true,
          })
          .click();
        if (save === "resume") {
          await page.reload();
          await expect(page.locator(".crawl-state")).toHaveText(state);
          await page
            .getByRole("button", { name: "Resume", exact: true })
            .click();
        }
        await expect(page.locator(".crawl-state")).toHaveText("running");
        await expect
          .poll(
            async () =>
              (await api(request, "detail", { crawl_id: record.id })).runs.at(
                -1,
              ).definition.network.user_agent,
          )
          .toBe(`updated-${token}`);
        const current = await api(request, "detail", { crawl_id: record.id });
        const checkpoint = before.runtime.runtime || run.summary;
        expect(current.runs).toHaveLength(1);
        expect(current.runs[0].id).toBe(run.id);
        expect(current.runs[0].created).toBe(run.created);
        expect(current.runs[0].elapsed_ns).toBeGreaterThanOrEqual(
          run.elapsed_ns,
        );
        expect(current.runs[0].definition.concurrency).toBe(4);
        expect(
          current.runtime.runtime.frontier.admitted,
        ).toBeGreaterThanOrEqual(checkpoint.frontier.admitted);
        expect(
          current.runtime.runtime.fetch.totals.successes,
        ).toBeGreaterThanOrEqual(checkpoint.fetch.totals.successes);
        await expect
          .poll(
            async () =>
              (
                await (
                  await request.get(
                    `${base}/fixture/request-events?user_agent=updated-${token}`,
                  )
                ).json()
              ).length,
          )
          .toBeGreaterThan(0);
        const compiled = await api(request, "pipeline", {
          crawl_id: record.id,
        });
        expect(
          compiled.configuration.config.http_client.per_origin_concurrency,
        ).toBe(4);
        await page.getByRole("button", { name: "Stop", exact: true }).click();
        await expect(page.locator(".crawl-state")).toHaveText("stopped");
        const final = await api(request, "detail", { crawl_id: record.id });
        expect(final.runs).toHaveLength(1);
        expect(final.runs[0].id).toBe(run.id);
        expect(final.runs[0].summary.fetch.totals.successes).toBeGreaterThan(
          run.summary?.fetch?.totals?.successes || 0,
        );
      } finally {
        await cleanup(request, record);
      }
    });

test("guided host concurrency accepts the upper boundary and persists it", async ({
  page,
  request,
}) => {
  await createForm(page, await info(request), {
    ...basic,
    name: "host upper bound",
    drain: true,
  });
  await page.getByText("Request pacing", { exact: true }).click();
  await page.getByLabel("Per-host concurrency", { exact: true }).fill("8192");
  await page.getByRole("button", { name: "Create", exact: true }).click();
  const record = await savedRecord(page, request);
  try {
    await page.reload();
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page.getByText("Request pacing", { exact: true }).click();
    await expect(
      page.getByLabel("Per-host concurrency", { exact: true }),
    ).toHaveValue("8192");
    await page
      .getByRole("button", { name: "Save and restart", exact: true })
      .click();
    await expect(page.locator(".crawl-state")).toHaveText("completed");
    expect(
      (await api(request, "pipeline", { crawl_id: record.id })).configuration
        .config.http_client.per_origin_concurrency,
    ).toBe(8192);
  } finally {
    await cleanup(request, record);
  }
});

test("paused draft survives application restart and resumes the same run", async ({
  page,
  request,
}) => {
  const fixture = await (await import("./globalSetup.mjs")).startFixture();
  const call = async (action, data = {}) => {
    const body = await (
      await request.post(`${fixture.url}/crawls/${action}`, { data })
    ).json();
    expect(body.success, body.error).toBe(true);
    return body.data;
  };
  try {
    const network = await (
      await request.get(`${fixture.url}/fixture/info`)
    ).json();
    await page.goto(fixture.url);
    await page
      .getByRole("button", { name: "＋ Create crawl", exact: true })
      .click();
    await page
      .getByLabel("Name", { exact: true })
      .fill("persistent paused draft");
    await page
      .getByLabel("Seed URLs", { exact: true })
      .fill(`${network.source}/site`);
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    await expect(page).toHaveURL(/\/crawls\/[a-f0-9-]+$/);
    const id = new URL(page.url()).pathname.split("/").at(-1);
    await expect(page.locator(".crawl-state")).toHaveText("running");
    await expect
      .poll(
        async () =>
          (await call("detail", { crawl_id: id })).runtime?.runtime?.fetch
            ?.totals?.successes || 0,
      )
      .toBeGreaterThan(0);
    await page.getByRole("button", { name: "Pause", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("paused");
    const before = await call("detail", { crawl_id: id }),
      run = before.runs.at(-1);
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page
      .getByLabel("Name", { exact: true })
      .fill("resumed saved configuration");
    const addedSeed = `${network.source}/article?new=${crypto.randomUUID()}`;
    await page.getByLabel("Seed URLs", { exact: true }).fill(addedSeed);
    await page.getByText("Request pacing", { exact: true }).click();
    await page.getByLabel("Per-host concurrency", { exact: true }).fill("4");
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await fixture.restart();
    await page.reload();
    await expect(page.locator(".crawl-state")).toHaveText("paused");
    await page.getByRole("button", { name: "Resume", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("running");
    await expect(
      page.getByRole("heading", {
        name: "resumed saved configuration",
        exact: true,
      }),
    ).toBeVisible();
    const after = await call("detail", { crawl_id: id });
    expect(after.runs).toHaveLength(1);
    expect(after.runs[0].id).toBe(run.id);
    expect(after.runs[0].elapsed_ns).toBeGreaterThanOrEqual(run.elapsed_ns);
    expect(after.runs[0].definition.concurrency).toBe(4);
    expect(after.runtime.runtime.fetch.totals.successes).toBeGreaterThanOrEqual(
      before.runtime.runtime.fetch.totals.successes,
    );
    await expect
      .poll(async () =>
        (await call("results", { crawl_id: id })).rows.some(
          (row) => row.uri === addedSeed,
        ),
      )
      .toBe(true);
  } finally {
    await fixture.stop();
  }
});
