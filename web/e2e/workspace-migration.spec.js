import { test, expect } from "./test";
const startFixture = async (...args) =>
  (await import("./globalSetup.mjs")).startFixture(...args);
import { readFile } from "node:fs/promises";
import path from "node:path";
test("legacy workspace limits and adaptive controls migrate into editable UI settings", async ({
  page,
  request,
}) => {
  const fixture = await startFixture({ legacy: true });
  try {
    const api = async (action) =>
      (
        await (
          await request.post(fixture.url + "/crawls/" + action, {
            data: { crawl_id: "fixture-legacy" },
          })
        ).json()
      ).data;
    await page.goto(fixture.url);
    await page
      .getByRole("button", { name: "Migrated crawl", exact: true })
      .click();
    await page.getByRole("link", { name: "Delver", exact: true }).click();
    await page
      .getByRole("button", { name: "Workspace configuration", exact: true })
      .click();
    await expect(
      page.getByLabel("Process concurrency", { exact: true }),
    ).toHaveValue("8");
    await expect(
      page.getByLabel("Shared host concurrency", { exact: true }),
    ).toHaveValue("1");
    await expect(
      page.getByLabel("Shared host delay", { exact: true }),
    ).toHaveValue("150ms");
    await page.goto(fixture.url + "/crawls/fixture-legacy");
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    const group = page.locator(".fetch-settings details").filter({
      has: page.locator("summary", { hasText: /^Adaptive control$/ }),
    });
    await group.locator("summary").click();
    await expect(
      page.getByLabel("Adjustment interval", { exact: true }),
    ).toHaveValue("1m0s");
    await expect(
      page.getByLabel("Concurrency increase step", { exact: true }),
    ).toHaveValue("2");
    await expect(
      page.getByLabel("Latency inflation", { exact: true }),
    ).toHaveCount(0);
    await page.getByLabel("Name", { exact: true }).fill("Migrated revised");
    await page
      .getByRole("button", { name: "Save and restart", exact: true })
      .click();
    await expect(page.locator(".crawl-state")).toHaveText("running");
    await expect
      .poll(
        async () =>
          (await api("detail")).runtime.runtime.fetch.totals.successes,
      )
      .toBeGreaterThan(0);
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "Migrated revised", exact: true }),
    ).toBeVisible();
    const compiled = await api("pipeline");
    expect(compiled.configuration.config.fetch.adjustment_interval).toBe(
      "1m0s",
    );
    expect(compiled.configuration.config.fetch).not.toHaveProperty(
      "latency_inflation",
    );
    const backup = JSON.parse(
      await readFile(
        path.join(fixture.storage, "migration-backups/crawls-v1.json"),
        "utf8",
      ),
    );
    expect(backup["fixture-legacy"].definition.fetch.latency_inflation).toBe(
      1.5,
    );
    expect(backup["fixture-legacy"].runs[0].summary.fetch.process_limit).toBe(
      0,
    );
    const migrated = await api("detail");
    expect(migrated.runs[0].id).toBe("fixture-history");
    expect(migrated.runs).toHaveLength(1);
    expect(
      migrated.runs[0].configuration_snapshot.definition.fetch
        .latency_inflation,
    ).toBe(1.5);

    await page.getByRole("button", { name: "Stop", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("stopped");
  } finally {
    await fixture.stop();
  }
});
