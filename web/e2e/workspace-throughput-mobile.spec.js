import { test, expect } from "./test";
import AxeBuilder from "@axe-core/playwright";
import options from "../src/views/page/fetchOptions.json";
import {
  createForm,
  info,
  savedRecord,
  api,
  cleanup,
} from "./helpers/workspace";
for (const mode of ["fixed", "adaptive"])
  test(`mobile throughput ${mode} settings and monitoring remain accessible`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await createForm(page, await info(request), {
      name: "mobile throughput",
      strategy: "host_diverse",
      source: "urls",
      connection: "direct",
      scope: "same_origin",
      drain: false,
    });
    await page
      .getByRole("radio", {
        name: mode === "fixed" ? "Fixed concurrency" : "Adaptive concurrency",
        exact: true,
      })
      .check();
    for (const group of new Set(
      options
        .filter(
          (o) =>
            mode === "adaptive" ||
            !(o.group.startsWith("Adaptive") || o.key.startsWith("cpu_")),
        )
        .map((o) => o.group),
    )) {
      const details = page.locator(".fetch-settings details").filter({
        has: page.locator("summary", { hasText: new RegExp(`^${group}$`) }),
      });
      if (!(await details.evaluate((e) => e.open)))
        await details.locator("summary").click();
    }
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth + 1,
      ),
    ).toBe(true);
    expect(
      (await new AxeBuilder({ page }).include(".crawl-workspace").analyze())
        .violations,
    ).toEqual([]);
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    const record = await savedRecord(page, request);
    try {
      await expect
        .poll(
          async () =>
            (await api(request, "detail", { crawl_id: record.id })).runtime
              ?.runtime?.fetch?.totals?.attempts || 0,
          { timeout: 15000 },
        )
        .toBeGreaterThan(0);
      await page.getByText("Details", { exact: true }).click();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth + 1,
        ),
      ).toBe(true);
      expect(
        (await new AxeBuilder({ page }).include(".crawl-workspace").analyze())
          .violations,
      ).toEqual([]);
    } finally {
      await cleanup(request, record);
    }
  });
