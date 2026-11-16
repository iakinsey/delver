import { test, expect } from "./test";
import {
  configurations,
  scenarioId,
  expectedScenarioCount,
} from "./configurationManifest.mjs";
import {
  base,
  api,
  info,
  createForm,
  savedRecord,
  cleanup,
} from "./helpers/workspace";

test.describe.configure({ mode: "parallel" });
const retentionLabels = [
  "Completed records",
  "Failed records",
  "URL deduplication",
];
function expiry(v) {
  return {
    finite: [],
    dedup: [2],
    completed: [0, 2],
    failed: [1, 2],
    permanent: [0, 1, 2],
  }[v];
}
console.log(`Required targeted UI interactions: ${expectedScenarioCount}`);
for (const v of configurations()) {
  test(`interaction ${scenarioId(v)}`, async ({ page, request }) => {
    const fixture = await info(request);
    const token = crypto.randomUUID(),
      agent = `Matrix/${token}`;
    let record;
    try {
      const source = await createForm(page, fixture, {
        ...v,
        name: token,
        path: `matrix/${token}`,
      });
      if (v.source !== "urls")
        await page
          .getByLabel("Feed URLs", { exact: true })
          .fill(`${source}/matrix-feed/${token}`);
      await page
        .getByRole("group", { name: "Concurrency control" })
        .getByRole("radio", {
          name:
            v.mode === "fixed" ? "Fixed concurrency" : "Adaptive concurrency",
          exact: true,
        })
        .check();
      await page
        .getByLabel("Respect robots.txt", { exact: true })
        .setChecked(v.robots);
      await page
        .getByLabel("Require proxy", { exact: true })
        .setChecked(v.requireProxy);
      if (v.connection === "socks5") {
        await page
          .getByLabel("Isolate proxy sessions", { exact: true })
          .setChecked(v.isolateSessions);
        if (v.isolateSessions) {
          const proxy = new URL(fixture.socks);
          proxy.username = "";
          proxy.password = "";
          await page
            .getByLabel("Proxy address", { exact: true })
            .fill(proxy.href);
        }
      }
      await page.getByText("URL filters", { exact: true }).click();
      await page
        .getByLabel("Restrict hostnames", { exact: true })
        .setChecked(v.restrictHosts);
      if (v.restrictHosts)
        await page
          .getByLabel("Allowed hostname suffixes", { exact: true })
          .fill("127.0.0.1\ncrawl-proxy.invalid");
      for (const i of expiry(v.retention))
        await page
          .getByRole("checkbox", {
            name: `${retentionLabels[i]}: no expiry`,
            exact: true,
          })
          .check();
      await page.getByText("Run limits", { exact: true }).click();
      if (v.requestLimit)
        await page.getByLabel("Fetch limit", { exact: true }).fill("4");
      if (v.timeLimit)
        await page.getByLabel("Time limit", { exact: true }).fill("750ms");
      await page.getByText("Requests", { exact: true }).click();
      await page.getByLabel("User-Agent", { exact: true }).fill(agent);
      await page.getByRole("button", { name: "Create", exact: true }).click();
      record = await savedRecord(page, request);
      const expected = structuredClone(record.definition);
      expect(expected).toMatchObject({
        scope: v.scope,
        strategy: v.strategy,
        fetch: { mode: v.mode },
        network: {
          respect_robots: v.robots,
          require_proxy: v.requireProxy,
          restrict_hosts: v.restrictHosts,
          isolate_proxy_sessions: v.isolateSessions,
        },
        termination: {
          when_drained: v.drain,
          max_requests: v.requestLimit ? 4 : 0,
          duration: v.timeLimit ? "750ms" : "",
        },
      });
      expect(record.runs).toEqual([]);
      await page.reload();
      await page.getByRole("button", { name: "Edit", exact: true }).click();
      await expect(page.getByLabel("Name", { exact: true })).toHaveValue(token);
      await expect(
        page.getByLabel("Respect robots.txt", { exact: true }),
      ).toBeChecked({ checked: v.robots });
      await expect(
        page.getByLabel("Require proxy", { exact: true }),
      ).toBeChecked({ checked: v.requireProxy });
      for (let i = 0; i < retentionLabels.length; i++)
        await expect(
          page.getByRole("checkbox", {
            name: `${retentionLabels[i]}: no expiry`,
            exact: true,
          }),
        ).toBeChecked({ checked: expiry(v.retention).includes(i) });
      await page.getByLabel("Name", { exact: true }).fill(token + "-edited");
      await page
        .getByRole("button", { name: "Save draft", exact: true })
        .click();
      await page
        .getByRole("button", { name: "Apply and restart", exact: true })
        .click();
      await expect
        .poll(
          async () =>
            (await api(request, "detail", { crawl_id: record.id })).runs.length,
        )
        .toBe(1);
      const started = await api(request, "detail", { crawl_id: record.id });
      expect(started.runs[0].definition).toEqual({
        ...expected,
        name: token + "-edited",
      });
      const pipeline = await api(request, "pipeline", { crawl_id: record.id });
      expect(pipeline.configuration.config.fetch.mode).toBe(v.mode);
      expect(pipeline.configuration.config.http_client).toMatchObject({
        user_agent: agent,
        respect_robots: v.robots,
      });
      expect(
        Boolean(pipeline.configuration.config.http_client.require_proxy),
      ).toBe(v.requireProxy);
      await expect
        .poll(
          async () =>
            (
              await api(request, "results", { crawl_id: record.id })
            ).rows.filter((row) => row.http_status === 200).length,
        )
        .toBeGreaterThan(0);
      const events = await (
        await request.get(
          `${base}/fixture/request-events?user_agent=${encodeURIComponent(agent)}`,
        )
      ).json();
      expect(events.some((row) => row.path.startsWith("/matrix/"))).toBe(true);
      expect(events.some((row) => row.path === "/robots.txt")).toBe(v.robots);
      if (v.restrictHosts)
        expect(
          events.every((row) =>
            /^(127\.0\.0\.1|crawl-proxy\.invalid)(:|$)/.test(row.host),
          ),
        ).toBe(true);
      if (v.drain || v.requestLimit || v.timeLimit)
        await expect(page.locator(".crawl-state")).toHaveText("completed");
      else {
        await page.getByRole("button", { name: "Stop", exact: true }).click();
        await expect(page.locator(".crawl-state")).toHaveText("stopped");
      }
      await test.info().attach("configuration-id", {
        body: scenarioId(v),
        contentType: "text/plain",
      });
    } finally {
      if (record) {
        await cleanup(request, record);
        const latest = await api(request, "detail", { crawl_id: record.id });
        await api(request, "delete", {
          crawl_id: record.id,
          revision: latest.revision,
          id: crypto.randomUUID(),
          confirm: true,
          preserve_logs: true,
        });
      }
    }
  });
}
