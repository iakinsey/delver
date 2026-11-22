import { test, expect } from "./test";
import AxeBuilder from "@axe-core/playwright";
const base = process.env.DELVER_E2E_BASE_URL;
async function api(request, action, data = {}) {
  const response = await request.post(`${base}/crawls/${action}`, { data });
  const body = await response.json();
  expect(body.success, body.error).toBe(true);
  return body.data;
}
test.describe.serial("multi-crawl workspace", () => {
  test("create a guided proxied crawl, finish, retain secrets, and edit without JSON", async ({
    page,
    request,
  }) => {
    const info = await (await request.get(`${base}/fixture/info`)).json();
    await page.goto(base);
    await expect(
      page.getByRole("button", { name: "＋ Create crawl", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "＋ Create crawl" }).click();
    await page
      .getByLabel("Name", { exact: true })
      .fill("Proxied documentation");
    await page
      .getByLabel("Seed URLs", { exact: true })
      .fill(`${info.source}/site\n${info.source}/article`);
    await expect(page.getByLabel("Seed URLs", { exact: true })).toHaveValue(
      `${info.source}/site\n${info.source}/article`,
    );
    await page.getByRole("radio", { name: /^Drain/ }).check();
    await page.getByRole("radio", { name: /^HTTP \/ HTTPS$/ }).check();
    const proxy = new URL(info.proxy);
    proxy.username = info.proxy_username;
    proxy.password = info.proxy_password;
    await page.getByLabel("Proxy address", { exact: true }).fill(proxy.href);
    await page
      .getByRole("checkbox", { name: "URL deduplication: no expiry" })
      .check();
    const accessibility = await new AxeBuilder({ page })
      .include(".crawl-workspace")
      .analyze();
    expect(accessibility.violations).toEqual([]);
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "Proxied documentation", exact: true }),
    ).toBeVisible();
    await expect(page.locator(".crawl-state.completed")).toBeVisible({
      timeout: 15000,
    });
    await page.getByText("Failed records", { exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Inspect", exact: true }),
    ).toBeDisabled();
    await expect(
      page.getByText("Runtime offline", { exact: true }),
    ).toBeVisible();
    await page.getByRole("link", { name: "Delver", exact: true }).click();
    const card = page
      .locator(".crawl-card")
      .filter({ hasText: "Proxied documentation" });
    await expect(card.locator(".crawl-card-metrics")).toContainText(
      "2 processed",
    );
    await page
      .getByRole("button", { name: "Proxied documentation", exact: true })
      .click();
    const list = await api(request, "list");
    expect(JSON.stringify(list)).toContain(info.proxy_password);
    const crawl = list.find(
      (c) => c.definition.name === "Proxied documentation",
    );
    expect(crawl.definition.proxy.url).toBe(proxy.href);
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await expect(page.getByLabel("Proxy address", { exact: true })).toHaveValue(
      proxy.href,
    );
    await expect(
      page.getByLabel("Proxy address", { exact: true }),
    ).toHaveAttribute("type", "text");
    await page
      .getByLabel("Name", { exact: true })
      .fill("Renamed documentation");
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await expect(page.getByText(/Draft saved/)).toBeVisible();
    await page
      .getByRole("button", {
        name: "Apply and restart",
        exact: true,
      })
      .click();
    await expect(
      page.getByRole("heading", { name: "Renamed documentation", exact: true }),
    ).toBeVisible({ timeout: 15000 });
    await expect(page.locator(".crawl-state.completed")).toBeVisible({
      timeout: 15000,
    });
  });
  test("monitoring crawls remain independent and the editor works on mobile", async ({
    page,
    request,
  }) => {
    const info = await (await request.get(`${base}/fixture/info`)).json(),
      defaults = await api(request, "defaults");
    const first = await api(request, "create", {
      definition: {
        ...defaults,
        name: "Monitor A",
        seeds: [`${info.source}/site`],
        revisit: "1s",
      },
    });
    const second = await api(request, "create", {
      definition: {
        ...defaults,
        name: "Monitor B",
        seeds: [`${info.source}/article`],
        revisit: "1s",
        strategy: "depth_first",
      },
    });
    await api(request, "start_run", {
      crawl_id: first.id,
      revision: first.revision,
    });
    await api(request, "start_run", {
      crawl_id: second.id,
      revision: second.revision,
    });
    await page.goto(`${base}/crawls/${first.id}`);
    await expect(page.locator(".crawl-state")).toHaveText("running", {
      timeout: 15000,
    });
    await page.getByRole("button", { name: "Pause", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("paused", {
      timeout: 15000,
    });
    await page.getByText("Failed records", { exact: true }).click();
    await page.getByRole("button", { name: "Inspect", exact: true }).click();
    await expect(page.getByText("Empty", { exact: true })).toBeVisible();
    const other = await api(request, "detail", { crawl_id: second.id });
    expect(other.runtime.state).toBe("running");
    await page.getByRole("button", { name: "Resume", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("running", {
      timeout: 15000,
    });
    await page.getByRole("button", { name: "Duplicate", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Monitor A copy", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole("radio", { name: /^SOCKS5$/ }).check();
    await page
      .getByLabel("Proxy address", { exact: true })
      .fill("socks5://operator:mobile-secret@127.0.0.1:1080");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth + 1,
      ),
    ).toBe(true);
    const accessibility = await new AxeBuilder({ page })
      .include(".crawl-workspace")
      .analyze();
    expect(accessibility.violations).toEqual([]);
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await expect(page.getByText(/Draft saved/)).toBeVisible();
  });
});
