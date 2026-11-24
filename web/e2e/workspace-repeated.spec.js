import { test, expect } from "./test";
import { api, info, createForm, savedRecord } from "./helpers/workspace";
const fields = [
  ["Independent sites", "minimum_sites", "3", "4"],
  ["Repeated structure similarity", "structure_similarity", "0.95", "0.96"],
  ["Visible-text similarity", "text_similarity", "0.85", "0.86"],
  ["Outgoing-destination similarity", "destination_similarity", "0.8", "0.81"],
  ["Host family dominance", "host_dominance", "0.8", "0.81"],
  ["Pages per site", "site_window", "20", "21"],
  ["Pages per family", "family_window", "100", "101"],
  ["Visible text limit (KiB)", "max_text_kib", "256", "257"],
  ["Distinct destination limit", "max_destinations", "256", "257"],
  ["Repeated admission share (%)", "admission_share_percent", "10", "11"],
];
async function form(page, request) {
  await createForm(page, await info(request), {
    name: "repeated family",
    strategy: "breadth_first",
    scope: "open_web",
    source: "urls",
    connection: "http",
    drain: true,
    path: "repeated-root",
  });
  await page
    .getByLabel("Seed URLs", { exact: true })
    .fill(
      [0, 1, 2]
        .map((n) => `http://repeated-mirror-${n}.invalid/repeated-root`)
        .join("\n"),
    );
  await page.getByText("Repeated pages", { exact: true }).click();
}
for (const mobile of [false, true])
  for (const mode of ["off", "observe", "deprioritize"])
    test(`repeated settings ${mode} ${mobile ? "mobile" : "desktop"}`, async ({
      page,
      request,
    }) => {
      if (mobile) await page.setViewportSize({ width: 390, height: 844 });
      await form(page, request);
      await expect(page.getByLabel("Repeated-page mode")).toHaveValue(
        "deprioritize",
      );
      await page.getByLabel("Repeated-page mode").selectOption(mode);
      if (mode !== "off")
        for (const [label, , value] of fields)
          await expect(page.getByLabel(label, { exact: true })).toHaveValue(
            value,
          );
      await page.getByRole("button", { name: "Create", exact: true }).click();
      const record = await savedRecord(page, request);
      await page.reload();
      await page.getByRole("button", { name: "Edit", exact: true }).click();
      await page.getByText("Repeated pages", { exact: true }).click();
      await expect(page.getByLabel("Repeated-page mode")).toHaveValue(mode);
      if (mode !== "off")
        for (const [label, , , value] of fields)
          await page.getByLabel(label, { exact: true }).fill(value);
      await page
        .getByRole("button", { name: "Save and restart", exact: true })
        .click();
      await expect(page.locator(".crawl-state")).toHaveText("completed", {
        timeout: 30000,
      });
      const compiled = JSON.parse(
        (await api(request, "pipeline", { crawl_id: record.id }))
          .configuration_json,
      ).resources.find((r) => r.type === "frontier_store").parameters
        .repeated_pages;
      expect(compiled.mode).toBe(mode);
      if (mode !== "off")
        for (const [, key, , value] of fields)
          expect(compiled[key]).toBe(Number(value));
    });
for (const mobile of [false, true])
  test(`repeated family inspection overrides analysis and folding ${mobile ? "mobile" : "desktop"}`, async ({
    page,
    request,
  }) => {
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    await form(page, request);
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    const record = await savedRecord(page, request);
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 30000,
    });
    const families = await api(request, "repeated_families", {
      crawl_id: record.id,
    });
    const family = families.families.find((f) => f.confirmed);
    expect(family).toBeTruthy();
    await page
      .getByRole("navigation", { name: "Crawl views" })
      .getByRole("button", { name: "Reports", exact: true })
      .click();
    await page.getByText("Repeated-page families", { exact: true }).click();
    await page
      .getByRole("button", { name: family.id.slice(0, 12), exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "Family members", exact: true }),
    ).toBeVisible();
    await page.getByLabel("Find family member").fill("mirror-1");
    await expect(
      page
        .locator("li")
        .filter({ hasText: "repeated-mirror-1.invalid" })
        .last(),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Exempt", exact: true }),
    ).toHaveCount(0);
    await page
      .getByRole("navigation", { name: "Crawl views" })
      .getByRole("button", { name: "Graph", exact: true })
      .click();
    await page.getByText("Generation parameters", { exact: true }).click();
    const fold = page.getByLabel("Fold repeated-page families", {
      exact: true,
    });
    await expect(fold).not.toBeChecked();
    await fold.check();
    await page.getByRole("button", { name: "Generate", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Regenerate", exact: true }),
    ).toBeEnabled({ timeout: 30000 });
    const initial = await api(request, "atlas_status", {
      crawl_id: record.id,
      hide_traps: true,
    });
    expect(initial.snapshot.options.fold_repeated_pages).toBe(true);
    await page
      .getByLabel("Find host", { exact: true })
      .fill("repeated-mirror-1");
    await page
      .getByRole("button", {
        name: `Family ${family.id.slice(0, 12)}`,
        exact: true,
      })
      .click();
    await expect(
      page.getByRole("heading", { name: "Family members (3)" }),
    ).toBeVisible();
    await page.getByLabel("Find member", { exact: true }).fill("mirror-2");
    await expect(
      page.getByText("repeated-mirror-2.invalid", { exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Zoom in", exact: true }).click();
    expect(
      (
        await api(request, "atlas_status", {
          crawl_id: record.id,
          hide_traps: true,
        })
      ).snapshot.id,
    ).toBe(initial.snapshot.id);
    await fold.uncheck();
    expect(
      (
        await api(request, "atlas_status", {
          crawl_id: record.id,
          hide_traps: true,
        })
      ).snapshot.id,
    ).toBe(initial.snapshot.id);
    await page
      .getByRole("navigation", { name: "Crawl views" })
      .getByRole("button", { name: "Crawl", exact: true })
      .click();
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page.getByText("Repeated-page families", { exact: true }).click();
    await page.getByRole("button", { name: "Exempt", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Re-enable", exact: true }),
    ).toBeEnabled({ timeout: 30000 });
    await page.getByRole("button", { name: "Re-enable", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Exempt", exact: true }),
    ).toBeEnabled();
    await page
      .getByRole("button", { name: "Analyze stored results", exact: true })
      .click();
    await expect(
      page.getByText("repeated analyze: completed", { exact: true }),
    ).toBeVisible({ timeout: 30000 });
  });

for (const mobile of [false, true])
  test(`repeated boundaries expert roundtrip conflicts and cancellation ${mobile ? "mobile" : "desktop"}`, async ({
    page,
    request,
  }) => {
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    await form(page, request);
    for (const [label, key] of fields) {
      const field = page.getByLabel(label, { exact: true });
      const min = Number(await field.getAttribute("min")),
        max = Number(await field.getAttribute("max"));
      for (const invalid of ["", String(min - 1), String(max + 1)]) {
        await field.fill(invalid);
        expect(await field.evaluate((e) => e.checkValidity())).toBe(false);
      }
      await field.fill(String(min));
      expect(await field.evaluate((e) => e.checkValidity())).toBe(true);
      await field.fill(String(max));
      expect(await field.evaluate((e) => e.checkValidity())).toBe(true);
      await field.fill(fields.find((v) => v[1] === key)[2]);
    }
    await page.getByLabel("Pages per site", { exact: true }).fill("101");
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("observation windows");
    await page.getByLabel("Pages per site", { exact: true }).fill("20");
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    const record = await savedRecord(page, request);
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 30000,
    });
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page.getByText("Worker pipeline", { exact: true }).click();
    await page
      .getByRole("button", { name: "Edit pipeline JSON", exact: true })
      .click();
    const editor = page.getByLabel("Worker pipeline JSON", { exact: true });
    const original = await editor.inputValue();
    const pipeline = JSON.parse(original);
    pipeline.resources.push({
      name: "repeated-opaque",
      type: "fixture_pressure",
      parameters: { token: "opaque-repeated", value: "EXACT_INTEGER" },
    });
    await editor.fill(
      JSON.stringify(pipeline).replace(
        '"EXACT_INTEGER"',
        "9007199254740993123",
      ),
    );
    await page.getByText("Repeated pages", { exact: true }).click();
    await page.getByLabel("Repeated-page mode").selectOption("observe");
    await page
      .getByRole("button", {
        name: "Apply repeated-page settings",
        exact: true,
      })
      .click();
    await expect(editor).toHaveValue(/9007199254740993123/);
    await expect(editor).toHaveValue(/"mode": "observe"/);
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    const family = (
      await api(request, "repeated_families", { crawl_id: record.id })
    ).families.find((f) => f.confirmed);
    expect(family).toBeTruthy();
    // Change the saved revision through the real API while this editor remains
    // open. The UI's stale override must fail visibly rather than overwrite it.
    const current = await api(request, "detail", { crawl_id: record.id });
    await api(request, "draft", {
      crawl_id: record.id,
      revision: current.revision,
      id: crypto.randomUUID(),
      configuration_json: (
        await api(request, "pipeline", { crawl_id: record.id })
      ).configuration_json,
    });
    await page.getByText("Repeated-page families", { exact: true }).click();
    await page.getByRole("button", { name: "Exempt", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("revision conflict");
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await page.reload();
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page.getByText("Repeated-page families", { exact: true }).click();
    const source = await info(request);
    const response = await request.post(
      `${source.workspace || new URL(page.url()).origin}/fixture/repeated-history`,
      { data: { crawl_id: record.id, count: 6000 } },
    );
    expect(response.status()).toBe(204);
    await page
      .getByRole("button", { name: "Analyze stored results", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Cancel analysis", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Resume analysis", exact: true }),
    ).toBeVisible({ timeout: 30000 });
    await page
      .getByRole("button", { name: "Resume analysis", exact: true })
      .click();
    await expect(
      page.getByText("repeated analyze: completed", { exact: true }),
    ).toBeVisible({ timeout: 45000 });
  });

for (const mobile of [false, true])
  test(`repeated member pagination and asynchronous retry ${mobile ? "mobile" : "desktop"}`, async ({
    page,
    request,
  }) => {
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    await form(page, request);
    await page
      .getByLabel("Seed URLs", { exact: true })
      .fill(
        Array.from(
          { length: 125 },
          (_, n) =>
            `http://repeated-many-${String(n).padStart(3, "0")}.invalid/repeated-root`,
        ).join("\n"),
      );
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    const record = await savedRecord(page, request);
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 30000,
    });
    const family = (
      await api(request, "repeated_families", { crawl_id: record.id })
    ).families.find((f) => f.confirmed);
    expect(family.sites).toBe(125);
    await page
      .getByRole("navigation", { name: "Crawl views" })
      .getByRole("button", { name: "Reports", exact: true })
      .click();
    await page.getByText("Repeated-page families", { exact: true }).click();
    await page
      .getByRole("button", { name: family.id.slice(0, 12), exact: true })
      .click();
    await page
      .getByRole("button", { name: "Next members", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Previous members", exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Previous members", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Previous members", exact: true }),
    ).toHaveCount(0);
    await page
      .getByRole("navigation", { name: "Crawl views" })
      .getByRole("button", { name: "Graph", exact: true })
      .click();
    await page.getByText("Generation parameters", { exact: true }).click();
    await page
      .getByLabel("Fold repeated-page families", { exact: true })
      .check();
    await page.getByRole("button", { name: "Generate", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Regenerate", exact: true }),
    ).toBeEnabled({ timeout: 30000 });
    await page
      .getByLabel("Find host", { exact: true })
      .fill("repeated-many-124");
    await page
      .getByRole("button", {
        name: `Family ${family.id.slice(0, 12)}`,
        exact: true,
      })
      .click();
    await expect(
      page.getByRole("heading", { name: "Family members (125)", exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Next members", exact: true })
      .click();
    await expect(
      page.getByText("repeated-many-124.invalid", { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Previous members", exact: true })
      .click();
    await page.getByLabel("Find member", { exact: true }).fill("124");
    await expect(
      page.getByText("repeated-many-124.invalid", { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("navigation", { name: "Crawl views" })
      .getByRole("button", { name: "Crawl", exact: true })
      .click();
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page.getByText("Repeated-page families", { exact: true }).click();
    const detail = await api(request, "detail", { crawl_id: record.id });
    await api(request, "repeated_override", {
      crawl_id: record.id,
      id: crypto.randomUUID(),
      revision: detail.revision,
      family: "f".repeat(64),
      exempt: true,
    });
    await expect(
      page.getByRole("button", { name: "Retry reclassification", exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Retry reclassification", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Retry reclassification", exact: true }),
    ).toHaveCount(2);
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await form(page, request);
    await page
      .getByLabel("Seed URLs", { exact: true })
      .fill(
        Array.from(
          { length: 30 },
          (_, n) =>
            `http://independent-${n}.invalid/repeated-root?independent=1`,
        ).join("\n"),
      );
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    await savedRecord(page, request);
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 30000,
    });
    await page
      .getByRole("navigation", { name: "Crawl views" })
      .getByRole("button", { name: "Reports", exact: true })
      .click();
    await page.getByText("Repeated-page families", { exact: true }).click();
    await page
      .getByRole("button", { name: "Next families", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "First families", exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "First families", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "First families", exact: true }),
    ).toHaveCount(0);
  });
