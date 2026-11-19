import { test, expect } from "./test";
import options from "../src/views/page/fetchOptions.json";
import {
  createForm,
  info,
  savedRecord,
  strategies,
  scopes,
  sources,
  connections,
  api,
  cleanup,
} from "./helpers/workspace";

test.describe.configure({ mode: "parallel" });
test("throughput catalogue stays synchronized with server defaults and rejects omitted controls", async ({
  request,
}) => {
  const defaults = await api(request, "defaults");
  expect(Object.keys(defaults.fetch).sort()).toEqual(
    ["mode", ...options.map((o) => o.key)].sort(),
  );
});

for (const mode of ["fixed", "adaptive"]) {
  test(`throughput ${mode} invalid numeric and duration boundaries block saving`, async ({
    page,
    request,
  }) => {
    await createForm(page, await info(request), {
      name: "invalid throughput",
      strategy: "host_diverse",
      source: "urls",
      connection: "direct",
      scope: "same_origin",
      drain: true,
    });
    await page
      .getByRole("radio", {
        name: mode === "fixed" ? "Fixed concurrency" : "Adaptive concurrency",
        exact: true,
      })
      .check();
    const visibleOptions = options.filter(
      (o) =>
        mode === "adaptive" ||
        !(o.group.startsWith("Adaptive") || o.key.startsWith("cpu_")),
    );
    for (const group of new Set(visibleOptions.map((o) => o.group))) {
      const details = page.locator(".fetch-settings details").filter({
        has: page.locator("summary", { hasText: new RegExp(`^${group}$`) }),
      });
      if (!(await details.evaluate((e) => e.open)))
        await details.locator("summary").click();
    }
    for (const option of visibleOptions) {
      const field = page.getByLabel(option.label, { exact: true });
      const original = await field.inputValue();
      for (const invalid of [option.min - 1, option.max + 1]) {
        await field.fill(
          option.kind.includes("duration") ? `${invalid}s` : String(invalid),
        );
        if (option.kind.includes("duration")) {
          await page
            .getByRole("button", {
              name: "Create",
              exact: true,
            })
            .click();
          await expect(page.getByRole("alert")).toContainText(option.label);
        } else
          expect(await field.evaluate((e) => e.checkValidity())).toBe(false);
        await expect(page).not.toHaveURL(/\/crawls\/[a-f0-9-]+$/);
      }
      await field.fill(original);
    }
  });
}

for (const mode of ["fixed", "adaptive"])
  for (const connection of connections)
    for (const inherited of [false, true]) {
      const robots = inherited,
        retries = inherited ? 1 : 0;
      test(`runtime throughput ${mode} ${connection} robots=${robots} inherited=${inherited} retries=${retries}`, async ({
        page,
        request,
      }) => {
        await createForm(page, await info(request), {
          name: "throughput runtime",
          strategy: "host_diverse",
          source: "urls",
          connection,
          scope: "same_origin",
          drain: true,
        });
        await page
          .getByRole("radio", {
            name:
              mode === "fixed" ? "Fixed concurrency" : "Adaptive concurrency",
            exact: true,
          })
          .check();
        await page
          .getByLabel("Concurrent requests overall", { exact: true })
          .fill("4");
        await page
          .getByLabel("Respect robots.txt", { exact: true })
          .setChecked(robots);
        if (mode === "adaptive") {
          await page
            .locator(".fetch-settings summary", {
              hasText: /^Adaptive limits$/,
            })
            .click();
          await page
            .getByLabel("Maximum concurrency", { exact: true })
            .fill("8");
        }
        await page
          .locator(".fetch-settings summary", {
            hasText: /^Network phases$/,
          })
          .click();
        for (const label of [
          "Connection timeout",
          "TLS handshake timeout",
          "Response header timeout",
          "Response body timeout",
        ])
          await page
            .getByLabel(label, { exact: true })
            .fill(inherited ? "inherit" : "2s");
        await page.getByText("Requests", { exact: true }).click();
        await page
          .getByLabel("Retries per request", { exact: true })
          .fill(String(retries));
        await page
          .getByRole("button", {
            name: "Create and start",
            exact: true,
          })
          .click();
        const record = await savedRecord(page, request);
        await expect(
          page.getByRole("region", {
            name: "Fetch metrics",
            exact: true,
          }),
        ).toBeVisible({ timeout: 15000 });
        await expect
          .poll(
            async () => {
              const detail = await api(request, "detail", {
                crawl_id: record.id,
              });
              return (
                (
                  detail.runtime?.runtime?.fetch ||
                  detail.runs.at(-1)?.summary?.fetch
                )?.totals?.attempts || 0
              );
            },
            { timeout: 20000 },
          )
          .toBeGreaterThan(0);
        const detail = await api(request, "detail", {
          crawl_id: record.id,
        });
        const metrics =
          detail.runtime?.runtime?.fetch || detail.runs.at(-1)?.summary?.fetch;
        expect(metrics.effective).toBeLessThanOrEqual(mode === "fixed" ? 4 : 8);
        expect(metrics.active).toBeLessThanOrEqual(metrics.effective);
        await expect
          .poll(
            async () =>
              (await api(request, "detail", { crawl_id: record.id })).runs.at(
                -1,
              )?.state,
            { timeout: 25000 },
          )
          .toBe("completed");
      });
    }

for (const mode of ["fixed", "adaptive"])
  for (const legacy of [false, true])
    test(`expert throughput ${mode} legacy=${legacy} applies explicitly and preserves opaque numbers`, async ({
      page,
      request,
    }) => {
      await createForm(page, await info(request), {
        name: "expert throughput",
        strategy: "host_diverse",
        source: "urls",
        connection: "direct",
        scope: "same_origin",
        drain: true,
      });
      await page
        .getByRole("button", {
          name: "Create",
          exact: true,
        })
        .click();
      const record = await savedRecord(page, request);
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
      sink.parameters = { opaque: "LARGE_NUMBER" };
      if (legacy) delete pipeline.config.fetch;
      await editor.fill(
        JSON.stringify(pipeline).replace(
          '"LARGE_NUMBER"',
          "9007199254740993123",
        ),
      );
      if (legacy)
        await page
          .getByRole("button", {
            name: "Configure",
            exact: true,
          })
          .click();
      await page
        .getByRole("radio", {
          name: mode === "fixed" ? "Fixed concurrency" : "Adaptive concurrency",
          exact: true,
        })
        .check();
      await page
        .getByLabel("Concurrent requests overall", { exact: true })
        .fill("6");
      await expect(editor).toHaveValue(/9007199254740993123/);
      // Controls remain a draft until Apply explicitly patches the pipeline.
      expect(
        JSON.parse(await editor.inputValue()).workers.find(
          (w) => w.type === "http_fetcher",
        ).count,
      ).not.toBe(6);
      let resumePatch;
      const patchGate = new Promise((resolve) => {
        resumePatch = resolve;
      });
      await page.route("**/crawls/patch_fetch", async (route) => {
        await patchGate;
        await route.continue();
      });
      await page
        .getByRole("button", {
          name: "Apply",
          exact: true,
        })
        .click();
      await expect(
        page.getByRole("button", { name: "Save draft", exact: true }),
      ).toBeDisabled();
      await expect(
        page.getByLabel("Concurrent requests overall", { exact: true }),
      ).toBeDisabled();
      await expect(editor).toBeDisabled();
      resumePatch();
      await expect
        .poll(
          async () =>
            JSON.parse(await editor.inputValue()).config?.fetch?.concurrency,
        )
        .toBe(6);
      await expect(editor).toHaveValue(/9007199254740993123/);
      const patched = JSON.parse(await editor.inputValue());
      expect(patched.config.fetch.mode).toBe(mode);
      expect(patched.config.fetch.concurrency).toBe(6);
      expect(patched.workers.find((w) => w.type === "http_fetcher").count).toBe(
        mode === "fixed" ? 6 : patched.config.fetch.maximum,
      );
      await page
        .getByRole("button", { name: "Save draft", exact: true })
        .click();
      await expect(page.getByText(/Draft saved/)).toBeVisible();
      expect(
        (await api(request, "pipeline", { crawl_id: record.id }))
          .configuration_json,
      ).toContain("9007199254740993123");
    });

for (const mode of ["fixed", "adaptive"])
  for (const inherit of [false, true])
    test(`all fetch controls accept nondefault values ${mode} inherit=${inherit}`, async ({
      page,
      request,
    }) => {
      await createForm(page, await info(request), {
        name: "all fetch controls",
        strategy: "host_diverse",
        source: "urls",
        connection: "direct",
        scope: "same_origin",
        drain: true,
      });
      await page
        .getByRole("radio", {
          name: mode === "fixed" ? "Fixed concurrency" : "Adaptive concurrency",
          exact: true,
        })
        .check();
      const visibleOptions = options.filter(
        (o) =>
          mode === "adaptive" ||
          !(o.group.startsWith("Adaptive") || o.key.startsWith("cpu_")),
      );
      for (const group of new Set(visibleOptions.map((o) => o.group))) {
        const details = page.locator(".fetch-settings details").filter({
          has: page.locator("summary", { hasText: new RegExp(`^${group}$`) }),
        });
        if (!(await details.evaluate((e) => e.open)))
          await details.locator("summary").click();
      }
      const defaults = (await api(request, "defaults")).fetch;
      const durations = {
        metrics_save_interval: "2s",
        sample_interval: "200ms",
        adjustment_interval: "2s",
        host_cooldown: "2s",
        host_cooldown_max: "20s",
        recovery_probe_interval: "3s",
        metrics_window: "20s",
      };
      const expected = { mode };
      for (const option of visibleOptions) {
        let value;
        if (option.kind === "inherit_duration")
          value = inherit ? "inherit" : "4s";
        else if (option.kind === "duration") value = durations[option.key];
        else
          value = Number(
            (
              defaults[option.key] + (option.kind === "integer" ? 1 : 0.01)
            ).toFixed(3),
          );
        await page
          .getByLabel(option.label, { exact: true })
          .fill(String(value));
        expected[option.key] = value === "inherit" ? "0s" : value;
      }
      await page
        .getByRole("button", {
          name: "Create",
          exact: true,
        })
        .click();
      const record = await savedRecord(page, request);
      expect(record.definition.fetch).toMatchObject(expected);
      await page.reload();
      await page.getByRole("button", { name: "Edit", exact: true }).click();
      const values = await page
        .locator(".fetch-settings input[aria-label]")
        .evaluateAll((nodes) =>
          Object.fromEntries(
            nodes.map((n) => [n.getAttribute("aria-label"), n.value]),
          ),
        );
      expect(values).toEqual(
        Object.fromEntries(
          visibleOptions.map((o) => [o.label, String(expected[o.key])]),
        ),
      );
    });

for (const mode of ["fixed", "adaptive"])
  test(`UI pressure recovery ${mode} retains the blocked run state`, async ({
    page,
    request,
  }) => {
    await createForm(page, await info(request), {
      name: "pressure recovery",
      strategy: "host_diverse",
      source: "urls",
      connection: "direct",
      scope: "same_origin",
      drain: true,
    });
    await page
      .getByRole("radio", {
        name: mode === "fixed" ? "Fixed concurrency" : "Adaptive concurrency",
        exact: true,
      })
      .check();
    await page.getByText("Resource limits", { exact: true }).click();
    await page
      .getByLabel("Memory high watermark (MiB)", { exact: true })
      .fill("1");
    await page
      .getByLabel("Memory resume watermark (MiB)", { exact: true })
      .fill("0");
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    const record = await savedRecord(page, request);
    try {
      await expect(
        page.getByRole("region", { name: "Fetch metrics", exact: true }),
      ).toContainText("memory_high", { timeout: 15000 });
      const blocked = await api(request, "detail", { crawl_id: record.id });
      expect(blocked.runtime.runtime.fetch.totals.attempts).toBe(0);
      await page.getByRole("button", { name: "Stop", exact: true }).click();
      await expect(page.locator(".crawl-state")).toHaveText("stopped", {
        timeout: 15000,
      });
      await page.getByRole("button", { name: "Edit", exact: true }).click();
      await page.getByText("Resource limits", { exact: true }).click();
      const defaults = (await api(request, "defaults")).fetch;
      await page
        .getByLabel("Memory high watermark (MiB)", { exact: true })
        .fill(String(defaults.memory_high_mb));
      await page
        .getByLabel("Memory resume watermark (MiB)", { exact: true })
        .fill(String(defaults.memory_low_mb));
      await page
        .getByRole("button", {
          name: "Save and restart",
          exact: true,
        })
        .click();
      await expect(page.locator(".crawl-state")).toHaveText("completed", {
        timeout: 20000,
      });
      const final = await api(request, "detail", { crawl_id: record.id });
      expect(final.runs).toHaveLength(1);
      expect(final.runs[0].id).toBe(blocked.runs[0].id);
      expect(final.runs[0].summary.fetch.totals.attempts).toBeGreaterThan(0);
    } finally {
      await cleanup(request, record);
    }
  });
