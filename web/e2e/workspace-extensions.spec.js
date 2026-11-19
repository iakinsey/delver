import { test, expect } from "./test";
import {
  base,
  api,
  info,
  createForm,
  savedRecord,
  cleanup,
} from "./helpers/workspace";
const formats = [
  [".zip,.pdf, .docx", [".zip", ".pdf", ".docx"]],
  ["zip, pdf, docx", [".zip", ".pdf", ".docx"]],
  ["zip, tar.gz,    pdf", [".zip", ".tar.gz", ".pdf"]],
  [".zip, .tar.gz, pdf,tar.xz", [".zip", ".tar.gz", ".pdf", ".tar.xz"]],
];
for (const [index, [input, extensions]] of formats.entries()) {
  test(`extension exclusions format ${index + 1} persists and prevents network requests`, async ({
    page,
    request,
  }) => {
    const fixture = await info(request),
      token = crypto.randomUUID(),
      path = `extension-test/${token}`,
      agent = `Extensions/${token}`;
    await createForm(page, fixture, {
      name: agent,
      strategy: "breadth_first",
      scope: "same_origin",
      source: "urls",
      connection: "direct",
      drain: true,
      path: path + "/root",
    });
    await page.getByText("URL filters", { exact: true }).click();
    await page
      .getByLabel("Excluded file extensions", { exact: true })
      .fill(input);
    await page
      .getByLabel("Seed URLs", { exact: true })
      .fill(
        `${fixture.source}/${path}/root\n${fixture.source}/${path}/seed.ZIP`,
      );
    await page.getByLabel("Respect robots.txt", { exact: true }).uncheck();
    await page.getByText("Requests", { exact: true }).click();
    await page.getByLabel("User-Agent", { exact: true }).fill(agent);
    await page.getByRole("button", { name: "Create", exact: true }).click();
    let record = await savedRecord(page, request);
    try {
      await page.reload();
      await page.getByRole("button", { name: "Edit", exact: true }).click();
      await page.getByText("URL filters", { exact: true }).click();
      await expect(
        page.getByLabel("Excluded file extensions", { exact: true }),
      ).toHaveValue(extensions.join(", "));
      await page
        .getByLabel("Excluded file extensions", { exact: true })
        .fill(input + ", .ZIP");
      await page
        .getByRole("button", { name: "Save and restart", exact: true })
        .click();
      await expect(page.locator(".crawl-state")).toHaveText("completed", {
        timeout: 30000,
      });
      const pipeline = await api(request, "pipeline", { crawl_id: record.id });
      expect(
        pipeline.configuration.config.http_client.excluded_extensions,
      ).toEqual(extensions);
      const rows = await (
        await request.get(
          `${base}/fixture/request-events?user_agent=${encodeURIComponent(agent)}`,
        )
      ).json();
      expect(rows.some((r) => r.path.endsWith("/archive.zip.html"))).toBe(true);
      expect(rows.some((r) => r.path.endsWith("/root"))).toBe(true);
      for (const row of rows)
        for (const extension of extensions)
          expect(row.path.toLowerCase().endsWith(extension)).toBe(false);
      record = await api(request, "detail", { crawl_id: record.id });
      expect(record.runs.at(-1).summary.frontier.failed_fetches).toBe(0);
      // Clear through browser controls and run the same retained frontier.
      const runID = record.runs.at(-1).id;
      await page.getByRole("button", { name: "Edit", exact: true }).click();
      await page.getByText("URL filters", { exact: true }).click();
      await page
        .getByLabel("Excluded file extensions", { exact: true })
        .fill("");
      await page
        .getByLabel("Seed URLs", { exact: true })
        .fill(`${fixture.source}/${path}/redirect`);
      await page
        .getByRole("button", { name: "Save and restart", exact: true })
        .click();
      await expect
        .poll(async () => {
          const events = await (
            await request.get(
              `${base}/fixture/request-events?user_agent=${encodeURIComponent(agent)}`,
            )
          ).json();
          return events.some((r) => r.path.endsWith("/archive.PDF"));
        })
        .toBe(true);
      record = await api(request, "detail", { crawl_id: record.id });
      expect(record.runs.at(-1).id).toBe(runID);
    } finally {
      await cleanup(request, record);
    }
  });
}
test("extension exclusions reject invalid input and preserve editor text", async ({
  page,
  request,
}) => {
  await createForm(page, await info(request), {
    name: "Invalid extensions",
    strategy: "breadth_first",
    scope: "same_origin",
    source: "urls",
    connection: "direct",
    drain: true,
  });
  await page.getByText("URL filters", { exact: true }).click();
  for (const value of [".", "*.pdf", "tar..gz", "foo/bar"]) {
    await page
      .getByLabel("Excluded file extensions", { exact: true })
      .fill(value);
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText(
      "invalid excluded file extension",
    );
    await expect(
      page.getByLabel("Excluded file extensions", { exact: true }),
    ).toHaveValue(value);
  }
});
test("configuration navigation separates settings from monitoring on mobile", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(base);
  await expect(
    page.getByLabel("Process concurrency", { exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Workspace configuration", exact: true })
    .focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/workspace\/configuration$/);
  await expect(
    page.getByLabel("Process concurrency", { exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByLabel("Shared host delay", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "← Crawls", exact: true }).click();
  await expect(
    page.getByLabel("Process concurrency", { exact: true }),
  ).toHaveCount(0);
  await createForm(page, await info(request), {
    name: "Mode controls",
    strategy: "breadth_first",
    scope: "same_origin",
    source: "urls",
    connection: "direct",
    drain: true,
  });
  await expect(
    page.getByLabel("Maximum concurrency", { exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("radio", { name: "Adaptive concurrency", exact: true })
    .check();
  await page
    .locator(".fetch-settings summary", { hasText: /^Adaptive limits$/ })
    .click();
  await expect(
    page.getByLabel("Maximum concurrency", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("radio", { name: "Fixed concurrency", exact: true })
    .check();
  await expect(
    page.getByLabel("Maximum concurrency", { exact: true }),
  ).toHaveCount(0);
});

test("extension edits invalidate URL preview and show the updated verdict", async ({
  page,
  request,
}) => {
  const fixture = await info(request);
  await createForm(page, fixture, {
    name: "Extension preview",
    strategy: "breadth_first",
    scope: "same_origin",
    source: "urls",
    connection: "direct",
    drain: true,
  });
  await page.getByText("URL filters", { exact: true }).click();
  await page
    .getByLabel("Test URL", { exact: true })
    .fill(fixture.source + "/file.PDF?download=1");
  await page.getByRole("button", { name: "Check URL", exact: true }).click();
  await expect(page.getByText("Allowed", { exact: true })).toBeVisible();
  await page
    .getByLabel("Excluded file extensions", { exact: true })
    .fill("pdf");
  await expect(page.getByText("Allowed", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Check URL", exact: true }).click();
  await expect(page.getByText("Excluded", { exact: true })).toBeVisible();
});

test("document exclusions respect robots and skip blocked feed sources", async ({
  page,
  request,
}) => {
  const fixture = await info(request),
    token = crypto.randomUUID(),
    agent = `RobotsExtensions/${token}`;
  await createForm(page, fixture, {
    name: agent,
    strategy: "breadth_first",
    scope: "same_origin",
    source: "both",
    connection: "direct",
    drain: true,
    path: "graph",
  });
  await page
    .getByLabel("Seed URLs", { exact: true })
    .fill(
      `${fixture.source}/graph\n${fixture.source}/extension-test/${token}/file.txt\n${fixture.source}/extension-test/${token}/file.xml`,
    );
  await page
    .getByLabel("Feed URLs", { exact: true })
    .fill(`${fixture.source}/feed\n${fixture.source}/filtered-feed.xml`);
  await page.getByText("URL filters", { exact: true }).click();
  await page
    .getByLabel("Excluded file extensions", { exact: true })
    .fill(".txt, .xml");
  await expect(
    page.getByLabel("Respect robots.txt", { exact: true }),
  ).toBeChecked();
  await page.getByText("Requests", { exact: true }).click();
  await page.getByLabel("User-Agent", { exact: true }).fill(agent);
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  const record = await savedRecord(page, request);
  try {
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 30000,
    });
    const rows = await (
      await request.get(
        `${base}/fixture/request-events?user_agent=${encodeURIComponent(agent)}`,
      )
    ).json();
    for (const path of ["/robots.txt", "/graph", "/feed", "/article"])
      expect(
        rows.some((r) => r.path === path),
        path,
      ).toBe(true);
    expect(rows.some((r) => r.path === "/robots-denied")).toBe(false);
    expect(rows.some((r) => r.path.endsWith("/file.txt"))).toBe(false);
    expect(rows.some((r) => r.path.endsWith("/file.xml"))).toBe(false);
    expect(rows.some((r) => r.path === "/filtered-feed.xml")).toBe(false);
    const final = await api(request, "detail", { crawl_id: record.id });
    expect(final.runs.at(-1).summary.fetch.counters.feed_policy_skips).toBe(1);
  } finally {
    await cleanup(request, record);
  }
});
