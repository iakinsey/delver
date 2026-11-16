import { test, expect } from "./test";
import {
  base,
  api,
  info,
  createForm,
  savedRecord,
  progress,
  cleanup,
} from "./helpers/workspace";
test.describe.configure({ mode: "parallel" });
const basic = {
  strategy: "host_diverse",
  scope: "same_origin",
  source: "urls",
  connection: "direct",
  drain: false,
};
for (const requestLimit of [false, true])
  for (const timeLimit of [false, true])
    test(`termination request=${requestLimit} duration=${timeLimit}`, async ({
      page,
      request,
    }) => {
      test.setTimeout(65000);
      await createForm(page, await info(request), {
        ...basic,
        name: `limits ${requestLimit} ${timeLimit}`,
        drain: !timeLimit,
        path: "site",
        revisit: "200ms",
      });
      await page.getByText("Run limits", { exact: true }).click();
      if (requestLimit)
        await page
          .getByLabel("Fetch limit", { exact: true })
          .fill(timeLimit ? "100" : "1");
      if (timeLimit)
        await page.getByLabel("Time limit", { exact: true }).fill("2s");
      await page
        .getByRole("button", { name: "Create and start", exact: true })
        .click();
      const record = await savedRecord(page, request);
      try {
        await expect(page.locator(".crawl-state")).toHaveText("completed", {
          timeout: 20000,
        });
        expect(await progress(page, "Processed")).toBeGreaterThan(0);
        if (requestLimit && !timeLimit) {
          expect(await progress(page, "Processed")).toBe(1);
          expect(await progress(page, "Frontier backlog")).toBeGreaterThan(0);
          await page
            .getByRole("button", {
              name: "Drain",
              exact: true,
            })
            .click();
          await expect(page.locator(".crawl-state")).toHaveText("completed", {
            timeout: 20000,
          });
          await expect
            .poll(() => progress(page, "Processed"), { timeout: 15000 })
            .toBe(2);
          expect(await progress(page, "Frontier backlog")).toBe(0);
        }
        await page.reload();
        await expect(page.locator(".crawl-state")).toHaveText("completed");
      } finally {
        await cleanup(request, record);
      }
    });
test("paused wall time does not consume active duration budget", async ({
  page,
  request,
}) => {
  test.setTimeout(45000);
  await createForm(page, await info(request), {
    ...basic,
    name: "paused duration",
    revisit: "200ms",
  });
  await page.getByText("Run limits", { exact: true }).click();
  await page.getByLabel("Time limit", { exact: true }).fill("15s");
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  const record = await savedRecord(page, request);
  try {
    await expect
      .poll(() => progress(page, "Processed"), { timeout: 15000 })
      .toBeGreaterThan(0);
    await page.getByRole("button", { name: "Pause", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("paused", {
      timeout: 10000,
    });
    const pausedElapsed = (
      await api(request, "detail", { crawl_id: record.id })
    ).runs.at(-1).elapsed_ns;
    await page.waitForTimeout(15500);
    await expect(page.locator(".crawl-state")).toHaveText("paused");
    expect(
      (await api(request, "detail", { crawl_id: record.id })).runs.at(-1)
        .elapsed_ns,
    ).toBe(pausedElapsed);
    await page.getByRole("button", { name: "Resume", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 30000,
    });
  } finally {
    await cleanup(request, record);
  }
});
test("bad feed reports source health until bounded run stops", async ({
  page,
  request,
}) => {
  test.setTimeout(45000);
  const fixture = await info(request);
  await createForm(page, fixture, {
    ...basic,
    source: "feeds",
    name: "bad feed",
    drain: true,
  });
  await page
    .getByLabel("Feed URLs", { exact: true })
    .fill(`${fixture.source}/feed-bad`);
  await page.getByText("Run limits", { exact: true }).click();
  await page.getByLabel("Time limit", { exact: true }).fill("4s");
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  const record = await savedRecord(page, request);
  try {
    await expect(page.getByRole("alert")).toContainText("starting_feeds", {
      timeout: 15000,
    });
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 15000,
    });
    expect(await progress(page, "Processed")).toBe(0);
  } finally {
    await cleanup(request, record);
  }
});
test("actual failed queue records can be paged and retried", async ({
  page,
  request,
}) => {
  test.setTimeout(90000);
  const fixture = await info(request),
    token = crypto.randomUUID();
  await createForm(page, fixture, {
    ...basic,
    name: `failed queue ${token}`,
    revisit: "24h",
  });
  await page
    .getByLabel("Seed URLs", { exact: true })
    .fill(
      Array.from(
        { length: 52 },
        (_, i) => `${fixture.source}/article?record=${i}`,
      ).join("\n"),
    );
  await page.getByRole("button", { name: "Create", exact: true }).click();
  const record = await savedRecord(page, request);
  try {
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page.getByText("Worker pipeline", { exact: true }).click();
    await page
      .getByRole("button", {
        name: "Edit pipeline JSON",
        exact: true,
      })
      .click();
    const editor = page.getByLabel("Worker pipeline JSON", { exact: true });
    const pipeline = JSON.parse(await editor.inputValue());
    const sink = pipeline.workers.find((w) => w.name === "save");
    sink.type = "fixture_failure";
    sink.parameters = { token };
    for (const r of pipeline.resources)
      if (r.type === "file_queue") r.parameters.max_attempts = 1;
    await editor.fill(JSON.stringify(pipeline, null, 2));
    await page
      .getByRole("button", {
        name: "Save and restart",
        exact: true,
      })
      .click();
    await expect
      .poll(
        async () => {
          const d = await api(request, "detail", { crawl_id: record.id });
          return d.runtime?.runtime?.queue_states?.results?.failed || 0;
        },
        { timeout: 40000 },
      )
      .toBe(52);
    await expect(page.getByRole("alert")).toContainText(
      "Failed pipeline records: 52",
      { timeout: 15000 },
    );
    await expect(
      page.getByText("Caught up. Monitoring continues.", { exact: true }),
    ).toHaveCount(0);
    await page.getByText("Failed records", { exact: true }).click();
    await page.getByLabel("Queue", { exact: true }).fill("results");
    await page.getByRole("button", { name: "Inspect", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Retry", exact: true }),
    ).toHaveCount(50);
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Retry", exact: true }),
    ).toHaveCount(2);
    await request.post(`${base}/fixture/recovery`, {
      data: { token, fail: false },
    });
    await page
      .getByRole("button", { name: "Retry", exact: true })
      .first()
      .click();
    await expect(
      page.getByRole("button", { name: "Retry", exact: true }),
    ).toHaveCount(1, { timeout: 15000 });
    await page.getByRole("button", { name: "Retry", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Retry", exact: true }),
    ).toHaveCount(50, { timeout: 15000 });
    await expect(
      page.getByRole("button", { name: "Next", exact: true }),
    ).toBeDisabled();
    await expect(
      page.getByRole("button", {
        name: "Previous",
        exact: true,
      }),
    ).toBeDisabled();
    await page
      .getByRole("button", { name: "Retry", exact: true })
      .first()
      .click();
    await expect(
      page.getByRole("button", { name: "Retry", exact: true }),
    ).toHaveCount(49, { timeout: 15000 });
    await expect(
      page.getByText("Custom output sink", { exact: true }),
    ).toBeVisible();
  } finally {
    await cleanup(request, record);
  }
});
