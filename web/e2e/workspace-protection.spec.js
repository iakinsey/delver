import { test, expect } from "./test";
import {
  base,
  setWorkspaceBase,
  info,
  createForm,
  savedRecord,
  api,
} from "./helpers/workspace";
const policy = async (request, action, data = {}) => {
  const r = await request.post(`${base}/workspace/protection`, {
    data: { action, ...data },
  });
  const b = await r.json();
  expect(b.success, b.error).toBe(true);
  return b.data;
};
test.describe("workspace protection", () => {
  test.describe.configure({ mode: "serial" });
  // Policy mutations intentionally affect every crawl in a workspace. Give this
  // suite a real isolated workspace rather than purging parallel tests' data.
  let fixture, previousBase;
  test.beforeAll(async () => {
    fixture = await (await import("./globalSetup.mjs")).startFixture();
    previousBase = base;
    setWorkspaceBase(fixture.url);
  });
  test.afterAll(async () => {
    if (previousBase) setWorkspaceBase(previousBase);
    if (fixture) await fixture.stop();
  });
  for (const layout of ["desktop", "mobile"]) {
    test(`protection toggles align with labels ${layout}`, async ({ page }) => {
      await page.setViewportSize(
        layout === "mobile"
          ? { width: 390, height: 844 }
          : { width: 1440, height: 1000 },
      );
      const aligned = async (input) => {
        await expect(input).toBeVisible();
        const bounds = await input.boundingBox();
        const label = await input.locator("..").boundingBox();
        expect(bounds.width).toBeLessThan(Math.min(48, label.width / 4));
        expect(bounds.x - label.x).toBeLessThan(16);
      };
      await page.goto(`${base}/workspace/configuration?view=blocklists`);
      await page.getByRole("button", { name: "Add list", exact: true }).click();
      for (const name of ["Pasted entries", "Feed URL"]) {
        const radio = page.getByRole("radio", { name, exact: true });
        await aligned(radio);
        await radio.locator("..").click();
        await expect(radio).toBeChecked();
      }
      const enabled = page.getByRole("checkbox", {
        name: "Enabled",
        exact: true,
      });
      await aligned(enabled);
      await enabled.locator("..").click();
      await expect(enabled).not.toBeChecked();
      await page.goto(`${base}/workspace/configuration?view=restricted`);
      const rule = page.getByRole("checkbox").first();
      await aligned(rule);
      await rule.locator("..").click();
      await expect(rule).not.toBeChecked();
      await page.goto(base);
      await page
        .getByRole("button", { name: "＋ Create crawl", exact: true })
        .click();
      await page.getByText("Trap detection", { exact: true }).click();
      const detect = page.getByRole("checkbox", {
        name: "Detect crawler traps",
        exact: true,
      });
      const automatic = page.getByRole("checkbox", {
        name: "Automatically exclude detected traps",
        exact: true,
      });
      await aligned(detect);
      await aligned(automatic);
      await detect.locator("..").click();
      await expect(automatic).toHaveCount(0);
      await detect.locator("..").click();
      await aligned(automatic);
    });
  }
  test("pasted lists preview, activation, matches, edit, toggles and deletion", async ({
    page,
    request,
  }) => {
    const name = `Local ${crypto.randomUUID()}`;
    await page.goto(`${base}/workspace/configuration?view=blocklists`);
    await page.getByRole("button", { name: "Add list", exact: true }).click();
    await page.getByLabel("List name", { exact: true }).fill(name);
    await page
      .getByLabel("Entries", { exact: true })
      .fill(
        "# note\nblocked-protection.test\nhttps://url-protection.test/private\n",
      );
    await page
      .getByRole("button", { name: "Preview list", exact: true })
      .click();
    await expect(
      page.getByRole("region", { name: "List preview" }),
    ).toContainText("2 entries");
    await page
      .getByRole("button", { name: "Activate and purge", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: `Disable ${name}`, exact: true }),
    ).toBeVisible();
    await page
      .getByLabel("Check URL", { exact: true })
      .fill("http://sub.blocked-protection.test/page");
    await page
      .getByRole("button", { name: "Check matches", exact: true })
      .click();
    await expect(
      page.getByRole("status").filter({ hasText: name }),
    ).toBeVisible();
    await page.reload();
    await expect(
      page.getByRole("button", { name: `Edit ${name}`, exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: `Edit ${name}`, exact: true })
      .click();
    await expect(page.getByLabel("Entries", { exact: true })).toHaveValue(
      /blocked-protection\.test/,
    );
    await page
      .getByLabel("Entries", { exact: true })
      .fill("blocked-protection.test\n*.invalid.test");
    await page
      .getByRole("button", { name: "Preview list", exact: true })
      .click();
    await expect(page.getByRole("alert")).toContainText("line 2");
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await page
      .getByRole("button", { name: `Disable ${name}`, exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: `Enable ${name}`, exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: `Enable ${name}`, exact: true })
      .click();
    await page
      .getByRole("button", { name: "Activate and purge", exact: true })
      .click();
    await page
      .getByRole("button", { name: `Delete ${name}`, exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: `Edit ${name}`, exact: true }),
    ).toHaveCount(0);
  });
  test("remote feed and failed refresh preserve active protection", async ({
    page,
    request,
  }) => {
    const f = await info(request),
      name = `Feed ${crypto.randomUUID()}`;
    await page.goto(`${base}/workspace/configuration?view=blocklists`);
    await page.getByRole("button", { name: "Add list", exact: true }).click();
    await page.getByLabel("List name", { exact: true }).fill(name);
    await page.getByRole("radio", { name: "Feed URL", exact: true }).check();
    await page
      .getByRole("textbox", { name: "Feed URL", exact: true })
      .fill(`${f.source}/protection/list`);
    await page.getByLabel("Refresh interval", { exact: true }).fill("10m");
    await page
      .getByRole("button", { name: "Preview list", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Activate and purge", exact: true })
      .click();
    await page
      .getByRole("button", { name: `Edit ${name}`, exact: true })
      .click();
    await expect(
      page.getByLabel("Refresh interval", { exact: true }),
    ).toHaveValue("10m");
    await page
      .getByRole("textbox", { name: "Feed URL", exact: true })
      .fill(`${f.source}/protection/unavailable-list`);
    await page
      .getByRole("button", { name: "Preview list", exact: true })
      .click();
    await expect(page.getByRole("alert")).toContainText("503");
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    expect(
      (
        await policy(request, "match", {
          query: "http://blocked-protection.test/",
        })
      ).sources.length,
    ).toBeGreaterThan(0);
    await page
      .getByRole("button", { name: `Delete ${name}`, exact: true })
      .click();
  });
  test("crawl detection controls and purge depth persist after editing and initiating execution", async ({
    page,
    request,
  }) => {
    const f = await info(request);
    await createForm(page, f, {
      name: `Settings ${crypto.randomUUID()}`,
      strategy: "breadth_first",
      scope: "open_web",
      source: "urls",
      connection: "http",
      drain: true,
      path: "protection/good",
    });
    await page.getByText("Trap detection", { exact: true }).click();
    for (const [label, value] of [
      ["Site observation window", "24"],
      ["Family observation window", "120"],
      ["Structure similarity", "0.75"],
      ["Link pattern redundancy", "0.85"],
      ["Maximum discovery novelty", "0.1"],
      ["Minimum fresh-link fraction", "0.6"],
      ["Minimum cross-site link fraction", "0.55"],
      ["Samples per confirmation window", "6"],
      ["Minimum links per page", "10"],
    ])
      await page.getByLabel(label, { exact: true }).fill(value);
    await page.getByText("URL filters", { exact: true }).click();
    await page
      .getByLabel("Restricted-content purge depth", { exact: true })
      .fill("2");
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    const record = await savedRecord(page, request);
    expect(record.definition.traps.site_window).toBe(24);
    expect(record.definition.restricted_purge_depth).toBe(2);
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 45000,
    });
    await page.reload();
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page.getByText("Trap detection", { exact: true }).click();
    await expect(
      page.getByLabel("Structure similarity", { exact: true }),
    ).toHaveValue("0.75");
    await page
      .getByRole("checkbox", {
        name: "Automatically exclude detected traps",
        exact: true,
      })
      .uncheck();
    await page.getByText("URL filters", { exact: true }).click();
    await expect(
      page.getByLabel("Restricted-content purge depth", { exact: true }),
    ).toHaveValue("2");
    await page
      .getByLabel("Restricted-content purge depth", { exact: true })
      .fill("0");
    await page
      .getByRole("button", { name: "Save and restart", exact: true })
      .click();
    await expect
      .poll(
        async () =>
          (await api(request, "detail", { crawl_id: record.id })).definition
            .restricted_purge_depth,
      )
      .toBe(0);
    const detail = await api(request, "detail", { crawl_id: record.id });
    expect(detail.definition.restricted_purge_depth).toBe(0);
    expect(detail.definition.traps.automatic).toBe(false);
  });
  test("restricted rules, memory, detection, purge progress and metadata export", async ({
    page,
    request,
  }) => {
    const name = `Synthetic ${crypto.randomUUID()}`,
      f = await info(request);
    await page.goto(`${base}/workspace/configuration?view=restricted`);
    await page.getByRole("button", { name: "Add rule", exact: true }).click();
    const index = (await policy(request, "state")).rules.length + 1;
    await page.getByLabel(`Rule name ${index}`, { exact: true }).fill(name);
    await page.getByLabel(`Phrases ${index}`, { exact: true }).fill("CPAN");
    await page.getByRole("button", { name: "Save rules", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("8–1024");
    await page
      .getByLabel(`Phrases ${index}`, { exact: true })
      .fill("synthetic restricted marker");
    await page.getByRole("button", { name: "Save rules", exact: true }).click();
    await expect(page.getByRole("alert")).toHaveCount(0);
    await page
      .getByLabel("Inspection memory budget (MiB)", { exact: true })
      .fill("128");
    await page
      .getByRole("button", { name: "Apply memory budget", exact: true })
      .click();
    await expect
      .poll(async () => (await policy(request, "state")).inspection_memory_mib)
      .toBe(128);
    await page.reload();
    await expect(
      page.getByLabel("Inspection memory budget (MiB)", { exact: true }),
    ).toHaveValue("128");
    await createForm(page, f, {
      name: `Restricted ${crypto.randomUUID()}`,
      strategy: "breadth_first",
      scope: "open_web",
      source: "urls",
      connection: "http",
      drain: true,
      path: "protection/restricted",
    });
    const host = `restricted-${crypto.randomUUID()}.invalid`;
    await page
      .getByLabel("Seed URLs", { exact: true })
      .fill(`http://${host}/protection/restricted`);
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    const record = await savedRecord(page, request);
    await expect
      .poll(
        async () => {
          const state = await policy(request, "state");
          return state.incidents.find((i) => i.crawl_id === record.id)?.state;
        },
        { timeout: 30000 },
      )
      .toBe("completed");
    await page.goto(`${base}/workspace/configuration?view=restricted`);
    await page.getByLabel("Search incidents", { exact: true }).fill(host);
    await page.getByRole("button", { name: "Search", exact: true }).click();
    await page
      .getByRole("button", { name: new RegExp(`${host}.*completed`) })
      .click();
    const report = page.getByRole("region", {
      name: "Incident report",
      exact: true,
    });
    await expect(report).toContainText(
      "Suspected restricted-content indicator",
    );
    await expect(report).toContainText("1 selected sites");
    const downloaded = page.waitForEvent("download");
    await report
      .getByRole("link", { name: "Export report", exact: true })
      .click();
    const download = await downloaded;
    const stream = await download.createReadStream();
    let text = "";
    for await (const chunk of stream) text += chunk;
    const exported = JSON.parse(text);
    expect(exported.state).toBe("completed");
    expect(exported.targets).toHaveLength(1);
    expect(text).not.toContain("synthetic restricted marker");
    expect(text).not.toContain("<html>");
    const rows = await api(request, "results", { crawl_id: record.id });
    expect(rows.rows).toHaveLength(0);
    await page
      .getByRole("button", { name: `Delete rule ${index}`, exact: true })
      .click();
    await page.getByRole("button", { name: "Save rules", exact: true }).click();
    await expect
      .poll(async () =>
        (await policy(request, "state")).rules.some((r) => r.name === name),
      )
      .toBe(false);
  });
  test("inactive feeds, refresh recovery, preset, keyboard and mobile configuration", async ({
    page,
    request,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    const f = await info(request),
      name = `Inactive ${crypto.randomUUID()}`,
      host = `feed-${crypto.randomUUID()}.test`;
    await page.goto(`${base}/workspace/configuration?view=blocklists`);
    await page.getByRole("button", { name: "Add list", exact: true }).click();
    await page.getByLabel("List name", { exact: true }).fill(name);
    const feed = page.getByRole("radio", { name: "Feed URL", exact: true });
    await feed.focus();
    await page.keyboard.press("Space");
    await page
      .getByRole("button", { name: "HaGeZi threat feed", exact: true })
      .click();
    await expect(
      page.getByRole("textbox", { name: "Feed URL", exact: true }),
    ).toHaveValue(/hagezi/);
    await page
      .getByRole("textbox", { name: "Feed URL", exact: true })
      .fill(`${f.source}/protection/unavailable-list`);
    await page
      .getByRole("button", { name: "Preview list", exact: true })
      .click();
    await expect(page.getByRole("alert")).toContainText("503");
    await page
      .getByRole("button", { name: "Save inactive feed", exact: true })
      .click();
    const row = page.getByRole("row").filter({
      has: page.getByRole("button", { name: `Edit ${name}`, exact: true }),
    });
    await expect(row).toContainText("Inactive");
    await expect(row).toContainText("503");
    await page
      .getByRole("button", { name: `Refresh ${name}`, exact: true })
      .click();
    await expect(page.getByRole("alert")).toContainText("503");
    await page
      .getByRole("button", { name: `Edit ${name}`, exact: true })
      .click();
    await page
      .getByRole("textbox", { name: "Feed URL", exact: true })
      .fill(`${f.source}/protection/list?domain=${host}`);
    await page
      .getByRole("button", { name: "Preview list", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Activate and purge", exact: true })
      .click();
    await expect(row).toContainText("Loaded");
    await page
      .getByRole("button", { name: `Refresh ${name}`, exact: true })
      .click();
    await expect(
      page.getByRole("region", { name: "List preview" }),
    ).toContainText("0 added");
    await page
      .getByRole("button", { name: "Activate and purge", exact: true })
      .click();
    expect(
      (await policy(request, "match", { query: `http://${host}/` })).sources
        .length,
    ).toBeGreaterThan(0);
    await page
      .getByRole("button", { name: `Delete ${name}`, exact: true })
      .click();
    await expect(row).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  });
  for (const mode of ["disabled", "observe", "exclude"]) {
    test(`trap network ${mode} preserves unrelated work and restoration`, async ({
      page,
      request,
    }) => {
      test.setTimeout(90000);
      const f = await info(request),
        family = `family-${crypto.randomUUID()}`;
      await createForm(page, f, {
        name: `Trap ${mode} ${family}`,
        strategy: "breadth_first",
        scope: "open_web",
        source: "urls",
        connection: "http",
        drain: true,
        path: "protection/good",
      });
      await page
        .getByLabel("Seed URLs", { exact: true })
        .fill(
          [0, 1, 2]
            .map(
              (n) =>
                `http://${family}-${n}.invalid/protection/trap?family=${family}&n=1`,
            )
            .join("\n") + `\nhttp://outside-${family}.invalid/protection/good`,
        );
      await page.getByText("Trap detection", { exact: true }).click();
      if (mode === "disabled")
        await page
          .getByRole("checkbox", { name: "Detect crawler traps", exact: true })
          .uncheck();
      if (mode === "observe")
        await page
          .getByRole("checkbox", {
            name: "Automatically exclude detected traps",
            exact: true,
          })
          .uncheck();
      await page.getByText("URL filters", { exact: true }).click();
      await page.getByLabel("Maximum depth", { exact: true }).fill("20");
      await page.getByLabel("Maximum host hops", { exact: true }).fill("20");
      await page.getByText("Run limits", { exact: true }).click();
      await page.getByLabel("Fetch limit", { exact: true }).fill("120");
      await page.getByText("Request pacing", { exact: true }).click();
      await page.getByLabel("Request delay", { exact: true }).fill("0s");
      await page
        .getByRole("button", { name: "Create and start", exact: true })
        .click();
      const record = await savedRecord(page, request);
      if (mode === "exclude") {
        await expect
          .poll(
            async () =>
              (await api(request, "trap_decisions", { crawl_id: record.id }))
                .length,
            { timeout: 45000 },
          )
          .toBe(3);
        await page.getByText("Trap exclusions", { exact: true }).click();
        await expect(
          page.getByRole("button", {
            name: `Restore ${family}-0.invalid`,
            exact: true,
          }),
        ).toBeVisible();
        const hosts = await api(request, "hosts", {
          crawl_id: record.id,
          query: `outside-${family}.invalid`,
        });
        expect(hosts.some((h) => h.visited > 0)).toBe(true);
        await page
          .getByRole("button", {
            name: `Restore ${family}-0.invalid`,
            exact: true,
          })
          .click();
        await expect
          .poll(
            async () =>
              (await api(request, "trap_decisions", { crawl_id: record.id }))
                .length,
          )
          .toBe(2);
      } else {
        await expect(page.locator(".crawl-state")).toHaveText(
          /completed|stopped/,
          { timeout: 60000 },
        );
        expect(
          await api(request, "trap_decisions", { crawl_id: record.id }),
        ).toHaveLength(0);
      }
    });
  }
  test("protection boundaries and cross-field errors preserve crawl controls", async ({
    page,
    request,
  }) => {
    const f = await info(request);
    await createForm(page, f, {
      name: `Boundaries ${crypto.randomUUID()}`,
      strategy: "breadth_first",
      scope: "open_web",
      source: "urls",
      connection: "http",
      drain: true,
      path: "protection/good",
    });
    await page.getByText("Trap detection", { exact: true }).click();
    const site = page.getByLabel("Site observation window", { exact: true });
    await site.fill("3");
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText(
      "invalid trap detection",
    );
    await expect(site).toHaveValue("3");
    await site.fill("20");
    const family = page.getByLabel("Family observation window", {
      exact: true,
    });
    await family.fill("19");
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText(
      "invalid trap detection",
    );
    await family.fill("100");
    for (const [label, value] of [
      ["Structure similarity", "0.49"],
      ["Minimum cross-site link fraction", "1.01"],
      ["Maximum discovery novelty", "-0.1"],
    ]) {
      const input = page.getByLabel(label, { exact: true }),
        before = await input.inputValue();
      await input.fill(value);
      expect(await input.evaluate((e) => e.checkValidity())).toBe(false);
      await input.fill(before);
    }
    await page.getByText("URL filters", { exact: true }).click();
    const depth = page.getByLabel("Restricted-content purge depth", {
      exact: true,
    });
    for (const value of ["-1", "0.5"]) {
      await depth.fill(value);
      expect(await depth.evaluate((e) => e.checkValidity())).toBe(false);
    }
    await depth.fill("3");
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    const record = await savedRecord(page, request);
    expect(record.definition.restricted_purge_depth).toBe(3);
  });
  test("purge failure, destination progress and retry remain visible in blocklists", async ({
    page,
    request,
  }) => {
    const f = await info(request),
      host = `purge-${crypto.randomUUID()}.invalid`,
      name = `Purge ${crypto.randomUUID()}`;
    await createForm(page, f, {
      name,
      strategy: "breadth_first",
      scope: "open_web",
      source: "urls",
      connection: "http",
      drain: true,
      path: "protection/good",
    });
    await page
      .getByLabel("Seed URLs", { exact: true })
      .fill(`http://${host}/protection/good`);
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    const record = await savedRecord(page, request);
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 30000,
    });
    expect(
      (
        await request.post(`${base}/fixture/protection-fault`, {
          data: { crawl_id: record.id, enabled: true },
        })
      ).status(),
    ).toBe(204);
    await page.goto(`${base}/workspace/configuration?view=blocklists`);
    await page.getByRole("button", { name: "Add list", exact: true }).click();
    await page.getByLabel("List name", { exact: true }).fill(name);
    await page.getByLabel("Entries", { exact: true }).fill(host);
    await page
      .getByRole("button", { name: "Preview list", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Activate and purge", exact: true })
      .click();
    await page
      .getByRole("button", { name: `${name} · failed`, exact: true })
      .click({ timeout: 30000 });
    const report = page.getByRole("region", {
      name: "Incident report",
      exact: true,
    });
    await expect(report).toContainText("Blocklist match");
    await expect(report.getByRole("alert")).toContainText("purge results");
    await expect(report).not.toContainText("Suspected restricted-content");
    expect(
      (
        await request.post(`${base}/fixture/protection-fault`, {
          data: { crawl_id: record.id, enabled: false },
        })
      ).status(),
    ).toBe(204);
    await report
      .getByRole("button", { name: "Retry purge", exact: true })
      .click();
    await expect(report).toContainText("completed", { timeout: 30000 });
    await expect(report).toContainText("verified");
    await expect(
      report.getByRole("button", { name: "Retry purge", exact: true }),
    ).toHaveCount(0);
    await page
      .getByRole("button", { name: `Delete ${name}`, exact: true })
      .click();
  });
  test("two blocklist editors reject revision conflicts without losing entered values", async ({
    page,
    context,
  }) => {
    const second = await context.newPage(),
      nameA = `Editor A ${crypto.randomUUID()}`,
      nameB = `Editor B ${crypto.randomUUID()}`;
    const prepare = async (p, name, domain) => {
      await p.goto(`${base}/workspace/configuration?view=blocklists`);
      await p.getByRole("button", { name: "Add list", exact: true }).click();
      await p.getByLabel("List name", { exact: true }).fill(name);
      await p.getByLabel("Entries", { exact: true }).fill(domain);
      await p
        .getByRole("button", { name: "Preview list", exact: true })
        .click();
    };
    await prepare(page, nameA, "revision-a.test");
    await prepare(second, nameB, "revision-b.test");
    await second
      .getByRole("button", { name: "Activate and purge", exact: true })
      .click();
    await expect(
      second.getByRole("button", { name: `Edit ${nameB}`, exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Activate and purge", exact: true })
      .click();
    await expect(page.getByRole("alert")).toContainText("revision conflict");
    await expect(page.getByLabel("List name", { exact: true })).toHaveValue(
      nameA,
    );
    await expect(page.getByLabel("Entries", { exact: true })).toHaveValue(
      "revision-a.test",
    );
    await page.getByRole("button", { name: "Reload", exact: true }).click();
    await page
      .getByRole("button", { name: "Preview list", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Activate and purge", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: `Edit ${nameA}`, exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: `Delete ${nameA}`, exact: true })
      .click();
    await second.getByRole("button", { name: "Reload", exact: true }).click();
    await second
      .getByRole("button", { name: `Delete ${nameB}`, exact: true })
      .click();
    await second.close();
  });
});
