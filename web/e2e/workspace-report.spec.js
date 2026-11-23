import { test, expect } from "./test";
import { base, api, info, createForm, savedRecord } from "./helpers/workspace";
async function crawl(page, request) {
  const fixture = await info(request);
  await createForm(page, fixture, {
    name: `Host report ${crypto.randomUUID()}`,
    strategy: "breadth_first",
    scope: "open_web",
    source: "urls",
    connection: "http",
    drain: true,
    path: "report-root",
  });
  await page.getByText("URL filters", { exact: true }).click();
  await page.getByLabel("Maximum host hops", { exact: true }).fill("12");
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  const record = await savedRecord(page, request);
  await expect(page.locator(".crawl-state")).toHaveText("completed", {
    timeout: 45000,
  });
  return record;
}
async function open(page, record) {
  await page.goto(`${base}/crawls/${record.id}/report`);
  await expect(page.getByLabel("Run report metrics")).toBeVisible();
}
async function choose(page, host = "crawl-proxy.invalid") {
  const field = page.getByRole("combobox", { name: "Search hosts" });
  await field.fill(host);
  await field.focus();
  await page
    .getByRole("option")
    .filter({ hasText: host })
    .getByRole("button")
    .click();
  await expect(
    page.getByRole("region", { name: "Directed host graph", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("status").filter({ hasText: "Calculating" }),
  ).toHaveCount(0, { timeout: 30000 });
}
for (const mobile of [false, true])
  test(`report metrics search importance and graph controls ${mobile ? "mobile" : "desktop"}`, async ({
    page,
    request,
  }, testInfo) => {
    test.setTimeout(90000);
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    const record = await crawl(page, request);
    await open(page, record);
    const report = await api(request, "report", { crawl_id: record.id });
    expect(report.counts.known).toBeGreaterThan(60);
    expect(report.counts.visited).toBe(report.counts.known);
    expect(report.counts.partial).toBe(false);
    await expect(page.getByText("URLs visited", { exact: true })).toBeVisible();
    await expect(
      page.getByText("URLs not visited", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText("Requests/s", { exact: true })).toBeVisible();
    await choose(page);
    const trapFilter = page.getByRole("checkbox", {
      name: "Hide trap-excluded hosts",
      exact: true,
    });
    await expect(trapFilter).toBeChecked();
    expect((await trapFilter.boundingBox()).width).toBeLessThan(40);
    const unfilteredResponse = page.waitForResponse(
      (response) =>
        response.url().endsWith("/crawls/host_graph") &&
        response.request().postDataJSON().hide_traps === false,
    );
    await trapFilter.uncheck();
    expect((await (await unfilteredResponse).json()).success).toBe(true);
    await trapFilter.check();
    await expect(page.getByText(/Size: authority/)).toBeVisible();
    await expect(page.getByText(/Authority percentile:/)).toBeVisible();
    await expect(page.getByText(/Graph rendering unavailable/)).toHaveCount(0);
    await page.screenshot({
      path: testInfo.outputPath("report.png"),
      fullPage: true,
    });
    expect(await page.locator(".report-canvas canvas").count()).toBeGreaterThan(
      0,
    );
    await page.getByLabel("Depth", { exact: true }).fill("0");
    await expect(page.getByText(/1 rendered \/ 1 hosts/)).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Grouped", exact: true }),
    ).toBeDisabled();
    await page.getByLabel("Depth", { exact: true }).fill("8");
    await expect(page.getByText(/70 rendered \/ 70 hosts/)).toBeVisible({
      timeout: 20000,
    });
    await page.getByText("Display settings", { exact: true }).click();
    await page.getByLabel("Rendered nodes", { exact: true }).fill("500");
    await page.getByLabel("Rendered links", { exact: true }).fill("32");
    await page.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(page.getByText(/Structural sample/)).toBeVisible();
    const bounded = await api(request, "host_graph", {
      crawl_id: record.id,
      host: "crawl-proxy.invalid",
      depth: 8,
      node_limit: 500,
      edge_limit: 32,
    });
    expect(bounded.nodes.length).toBeLessThanOrEqual(33);
    const reached = new Set(
      bounded.nodes.filter((n) => n.selected).map((n) => n.id),
    );
    for (let pass = 0; pass < bounded.nodes.length; pass++)
      for (const edge of bounded.edges) {
        if (reached.has(edge.source) || reached.has(edge.target)) {
          reached.add(edge.source);
          reached.add(edge.target);
        }
      }
    expect(reached.size).toBe(bounded.nodes.length);
    await page.getByLabel("Rendered nodes", { exact: true }).fill("20");
    await page.getByLabel("Rendered links", { exact: true }).fill("64");
    await page.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(page.getByText(/Structural sample/)).toBeVisible();
    await page.getByRole("button", { name: "Grouped", exact: true }).click();
    await expect(page.getByText(/ · Grouped$/)).toBeVisible();
    let graph = await api(request, "host_graph", {
      crawl_id: record.id,
      host: "crawl-proxy.invalid",
      depth: 8,
      mode: "grouped",
      node_limit: 20,
      edge_limit: 64,
    });
    const group = graph.nodes.find((n) => n.group);
    expect(group).toBeTruthy();
    await page.getByText("Visible hosts and groups", { exact: true }).click();
    await page
      .locator(".report-visible")
      .getByRole("button", { name: group.name, exact: true })
      .first()
      .click();
    await page.getByRole("button", { name: "Expand", exact: true }).click();
    await expect(page.getByText(/Calculating…/)).toHaveCount(0, {
      timeout: 20000,
    });
    await expect(page.getByRole("alert")).toHaveCount(0);
    await choose(page, "deep-7.test");
    await expect(page.locator(".report-node-details h2")).toHaveText(
      "deep-7.test",
    );
    await page.getByRole("button", { name: "Zoom in", exact: true }).click();
    await page.getByRole("button", { name: "Zoom out", exact: true }).click();
    await page.getByRole("button", { name: "Fit", exact: true }).click();
    await page
      .getByRole("region", { name: "Directed host graph", exact: true })
      .focus();
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("+");
    await page.keyboard.press("-");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    expect(errors).toEqual([]);
  });
test("report keyboard search invalid depth stale requests and navigation", async ({
  page,
  request,
}) => {
  test.setTimeout(90000);
  const record = await crawl(page, request);
  await open(page, record);
  const q = page.getByRole("combobox", { name: "Search hosts" });
  await q.focus();
  await expect(page.getByRole("option")).toHaveCount(20);
  await expect(page.getByRole("option").first()).toContainText(
    "crawl-proxy.invalid",
  );
  const ranked = await api(request, "hosts", {
    crawl_id: record.id,
    limit: 100,
  });
  expect(ranked[0].name).toBe("crawl-proxy.invalid");
  expect(ranked[0].connections).toBe(61);
  await page.getByRole("button", { name: "More hosts", exact: true }).click();
  await expect(page.getByRole("option")).toHaveCount(40);
  const names = await page
    .getByRole("option")
    .locator("button")
    .evaluateAll((buttons) => buttons.map((b) => b.firstChild.textContent));
  expect(names).toEqual(ranked.slice(0, 40).map((h) => h.name));
  expect(new Set(names).size).toBe(40);
  await q.fill("report-a");
  await expect(page.getByRole("option")).toHaveCount(20);
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("region", { name: "Directed host graph", exact: true }),
  ).toBeVisible();
  await page.getByLabel("Depth", { exact: true }).fill("-1");
  await expect(page.getByRole("alert")).toContainText("Depth must be");
  await page.getByLabel("Depth", { exact: true }).fill("2");
  await expect(page.getByRole("alert")).toHaveCount(0, { timeout: 20000 });
  await q.fill("zzzz-not-present");
  await q.focus();
  await expect(page.getByRole("option")).toHaveCount(0);
  await page.keyboard.press("Escape");
  await choose(page, "bridge.test");
  await page.getByLabel("Depth", { exact: true }).fill("1");
  await page.getByLabel("Depth", { exact: true }).fill("8");
  await choose(page, "deep-7.test");
  await expect(page.locator(".report-node-details h2")).toHaveText(
    "deep-7.test",
  );
  // The overlay must use the same coordinate space as Sigma after layout.
  // Select another inspector entry, then click the selected-host ring center.
  await page.waitForTimeout(1500); // bounded layout stops after 1200ms
  const ring = await page.locator(".report-rings").evaluate((canvas) => {
    const { data } = canvas
      .getContext("2d")
      .getImageData(0, 0, canvas.width, canvas.height);
    let x = 0,
      y = 0,
      count = 0;
    for (let row = 0; row < canvas.height; row++)
      for (let col = 0; col < canvas.width; col++) {
        const i = (row * canvas.width + col) * 4;
        if (
          Math.abs(data[i] - 82) < 8 &&
          Math.abs(data[i + 1] - 189) < 8 &&
          data[i + 2] > 245 &&
          data[i + 3] > 180
        ) {
          x += col;
          y += row;
          count++;
        }
      }
    if (!count) throw Error("Selected host ring absent");
    return { x: x / count / canvas.width, y: y / count / canvas.height };
  });
  await page.getByText("Visible hosts and groups", { exact: true }).click();
  const other = page
    .locator(".report-visible button")
    .filter({ hasText: "crawl-proxy.invalid" });
  await other.click();
  await expect(page.locator(".report-node-details h2")).toHaveText(
    "crawl-proxy.invalid",
  );
  await page.locator(".report-canvas-shell").scrollIntoViewIfNeeded();
  const box = await page.locator(".report-rings").boundingBox();
  await page.mouse.click(
    box.x + ring.x * box.width,
    box.y + ring.y * box.height,
  );
  await expect(page.locator(".report-node-details h2")).toHaveText(
    "deep-7.test",
  );
  await page.getByRole("button", { name: "Crawl", exact: true }).click();
  await expect(page).toHaveURL(`${base}/crawls/${record.id}`);
  await page
    .locator("section")
    .filter({
      has: page.getByRole("heading", { name: "Run history", exact: true }),
    })
    .getByRole("button", { name: "Reports", exact: true })
    .last()
    .click();
  await expect(page).toHaveURL(/run_id=/);
  await expect(page.getByLabel("Run report metrics")).toBeVisible();
  await page
    .getByRole("navigation", { name: "Crawl views" })
    .getByRole("button", { name: "Graph", exact: true })
    .click();
  await expect(page).toHaveURL(/\/network\?run_id=/);
  await page
    .getByRole("navigation", { name: "Crawl views" })
    .getByRole("button", { name: "Reports", exact: true })
    .click();
  await expect(page).toHaveURL(/\/report\?run_id=/);
  await expect(page.getByLabel("Run report metrics")).toBeVisible();
});

test("report reconnect restart and retained run identity", async ({
  page,
  request,
  context,
}) => {
  test.setTimeout(90000);
  const record = await crawl(page, request);
  await open(page, record);
  await choose(page, "bridge.test");
  await context.setOffline(true);
  await expect(page.getByRole("alert")).toBeVisible({ timeout: 15000 });
  await context.setOffline(false);
  await expect(page.getByRole("alert")).toHaveCount(0, { timeout: 15000 });
  await page.getByRole("button", { name: "Crawl", exact: true }).click();
  await page.getByRole("button", { name: "Restart", exact: true }).click();
  await expect(page.locator(".crawl-state")).toHaveText("completed", {
    timeout: 45000,
  });
  const detail = await api(request, "detail", { crawl_id: record.id });
  expect(detail.runs.length).toBe(1);
  await page.goto(`${base}/crawls/${record.id}/report`);
  await expect(page.getByLabel("Run report metrics")).toBeVisible();
  await choose(page, "bridge.test");
  await page.goto(
    `${base}/crawls/${record.id}/report?run_id=${detail.runs[0].id}`,
  );
  await expect(page.getByLabel("Run report metrics")).toBeVisible();
  await expect(
    page.getByRole("combobox", { name: "Search hosts" }),
  ).toHaveValue("");
  await expect(
    page.getByRole("region", { name: "Directed host graph", exact: true }),
  ).toHaveCount(0);
  const first = await api(request, "report", {
    crawl_id: record.id,
    run_id: detail.runs[0].id,
  });
  const second = await api(request, "report", {
    crawl_id: record.id,
    run_id: detail.runs[0].id,
  });
  expect(first.counts.known).toBe(second.counts.known);
  expect(first.run_id).toBe(second.run_id);
});
