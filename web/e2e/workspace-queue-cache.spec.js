import { test, expect } from "./test";
import { api, info, createForm, savedRecord } from "./helpers/workspace";
const basic = {
  name: "queue cache",
  strategy: "host_diverse",
  scope: "same_origin",
  source: "urls",
  connection: "direct",
  drain: true,
};
for (const mobile of [false, true])
  test(`request queue cache guided persistence and runtime ${mobile ? "mobile" : "desktop"}`, async ({
    page,
    request,
  }) => {
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    await createForm(page, await info(request), basic);
    const field = page.getByLabel("Request queue cache (MiB)", { exact: true });
    await expect(field).toHaveValue("64");
    for (const invalid of ["", "15", "1025", "16.5"]) {
      await field.fill(invalid);
      expect(await field.evaluate((e) => e.checkValidity())).toBe(false);
    }
    await field.fill("16");
    await page.getByRole("button", { name: "Create", exact: true }).click();
    const record = await savedRecord(page, request);
    await page.reload();
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await expect(field).toHaveValue("16");
    await field.fill("1024");
    await page
      .getByRole("button", { name: "Save and restart", exact: true })
      .click();
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 30000,
    });
    const pipeline = await api(request, "pipeline", { crawl_id: record.id });
    expect(
      JSON.parse(pipeline.configuration_json).resources.find(
        (r) => r.type === "crawl_queue",
      ).parameters.cache_budget_mb,
    ).toBe(1024);
    const detail = await api(request, "detail", { crawl_id: record.id });
    const fetch =
      detail.runtime?.runtime?.fetch || detail.runs.at(-1)?.summary?.fetch;
    expect(fetch.pressure.resource_metrics.requests.cache_capacity_bytes).toBe(
      1024 * 1024 * 1024,
    );
    const metrics = page.getByRole("region", {
      name: "Fetch metrics",
      exact: true,
    });
    await metrics.locator("summary").click();
    const capacity = metrics.getByText(/^requests: cache capacity bytes:/);
    await expect(capacity).toBeVisible();
    expect(
      Number((await capacity.innerText()).split(":").at(-1).replace(/\D/g, "")),
    ).toBe(1024 * 1024 * 1024);
    await expect(metrics.getByText(/^Inspection:/)).toBeVisible();
    await expect(metrics.getByText(/^Inspection:/)).toContainText("bytes peak");
    await expect(metrics.getByText(/directory scans:/).first()).toBeVisible();
    await expect(
      metrics.getByRole("cell", { name: "inspection wait", exact: true }),
    ).toBeVisible();
  });
for (const mobile of [false, true])
  test(`request queue cache expert exact integers ${mobile ? "mobile" : "desktop"}`, async ({
    page,
    request,
  }) => {
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    await createForm(page, await info(request), basic);
    await page.getByRole("button", { name: "Create", exact: true }).click();
    const record = await savedRecord(page, request);
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page.getByText("Worker pipeline", { exact: true }).click();
    await page
      .getByRole("button", { name: "Edit pipeline JSON", exact: true })
      .click();
    const editor = page.getByLabel("Worker pipeline JSON", { exact: true });
    const pipeline = JSON.parse(await editor.inputValue());
    pipeline.resources.push({
      name: "cache-metadata",
      type: "fixture_pressure",
      parameters: { token: "cache-metadata", opaque: "LARGE_INTEGER" },
    });
    await editor.fill(
      JSON.stringify(pipeline).replace(
        '"LARGE_INTEGER"',
        "9007199254740993123",
      ),
    );
    const field = page.getByLabel("Request queue cache (MiB): requests", {
      exact: true,
    });
    await field.fill("32");
    let release;
    const gate = new Promise((resolve) => {
      release = resolve;
    });
    await page.route("**/crawls/patch_queue_cache", async (route) => {
      await gate;
      await route.continue();
    });
    await page
      .getByRole("button", { name: "Apply queue cache", exact: true })
      .click();
    await expect(editor).toBeDisabled();
    await expect(
      page.getByRole("radio", { name: "Fixed concurrency", exact: true }),
    ).toBeDisabled();
    release();
    await expect(
      page.getByRole("button", { name: "Apply queue cache", exact: true }),
    ).toBeHidden();
    await expect(editor).toHaveValue(/9007199254740993123/);
    expect(
      JSON.parse(await editor.inputValue()).resources.find(
        (r) => r.type === "crawl_queue",
      ).parameters.cache_budget_mb,
    ).toBe(32);
    await page
      .getByRole("button", { name: "Save and restart", exact: true })
      .click();
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 30000,
    });
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await expect(editor).toHaveValue(/9007199254740993123/);
    await expect(field).toHaveValue("32");
    const raw = await api(request, "pipeline", { crawl_id: record.id });
    expect(raw.configuration_json).toContain("9007199254740993123");
  });
