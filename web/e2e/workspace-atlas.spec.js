import { test, expect } from "./test";
import { base, api, info, createForm, savedRecord } from "./helpers/workspace";
for (const mobile of [false, true])
  test(`generated network ${mobile ? "mobile" : "desktop"}`, async ({
    page,
    request,
  }, testInfo) => {
    test.setTimeout(90000);
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await createForm(page, await info(request), {
      name: `Network ${crypto.randomUUID()}`,
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
    await page.goto(`${base}/crawls/${record.id}/report`);
    await page
      .getByRole("button", { name: "Interactive network", exact: true })
      .click();
    await expect(page).toHaveURL(/\/network$/);
    for (const [label, suffix] of [
      ["Crawl", ""],
      ["Reports", "/report"],
      ["Graph", "/network"],
    ]) {
      await page
        .getByRole("navigation", { name: "Crawl views" })
        .getByRole("button", { name: label, exact: true })
        .click();
      await expect(page).toHaveURL(`${base}/crawls/${record.id}${suffix}`);
      await expect(
        page
          .getByRole("navigation", { name: "Crawl views" })
          .getByRole("button"),
      ).toHaveText(["Crawl", "Reports", "Graph"]);
      await expect(
        page.getByRole("button", {
          name: "Workspace configuration",
          exact: true,
        }),
      ).toHaveCount(0);
      await expect(
        page.getByRole("button", { name: /Create crawl/ }),
      ).toHaveCount(0);
      await expect(
        page.getByRole("button", { name: "← Crawls", exact: true }),
      ).toHaveCount(0);
      await expect(
        page
          .getByRole("navigation", { name: "Crawl views" })
          .getByRole("button", { name: label, exact: true }),
      ).toHaveAttribute("aria-current", "page");
    }
    await expect(page.getByText("No snapshot", { exact: true })).toBeVisible();
    await page.getByText("Generation parameters", { exact: true }).click();
    await page.getByLabel("Host limit", { exact: true }).fill("10");
    await page.getByRole("button", { name: "Generate", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("host limit exceeded");
    await page.getByLabel("Host limit", { exact: true }).fill("200000");
    await page.getByRole("button", { name: "Generate", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Regenerate", exact: true }),
    ).toBeEnabled();
    const canvas = page.getByLabel("Interactive host network", { exact: true });
    await expect(canvas).toBeVisible();
    await expect(
      page.getByRole("status").filter({ hasText: /hosts in view/ }),
    ).toBeVisible();
    const status = await api(request, "atlas_status", { crawl_id: record.id });
    expect(status.snapshot.hosts).toBeGreaterThan(50);
    const frame = await api(request, "atlas_view", {
      crawl_id: record.id,
      snapshot: status.snapshot.id,
      bounds: [-1, -1, 2, 2],
      node_limit: 5000,
      edge_limit: 20000,
    });
    expect(frame.nodes.length).toBe(status.snapshot.hosts);
    // Use actual server coordinates and the canvas camera transform to click.
    const chosen = frame.nodes.find((n) => n.host_id);
    const box = await canvas.boundingBox();
    await canvas.evaluate((c) => {
      const descriptor = Object.getOwnPropertyDescriptor(
        HTMLCanvasElement.prototype,
        "width",
      );
      c._bufferResizes = 0;
      Object.defineProperty(c, "width", {
        get() {
          return descriptor.get.call(this);
        },
        set(v) {
          this._bufferResizes++;
          descriptor.set.call(this, v);
        },
      });
    });
    await canvas.click({
      position: {
        x: (chosen.x - 0.5) * Math.min(box.width, box.height) + box.width / 2,
        y: (chosen.y - 0.5) * Math.min(box.width, box.height) + box.height / 2,
      },
    });
    await expect(page.locator(".network-hostname")).toBeVisible();
    await expect(page.getByText("Authority", { exact: true })).toBeVisible();
    await page
      .getByLabel("Connections", { exact: true })
      .selectOption("incoming");
    await expect(page.getByLabel("Connections", { exact: true })).toHaveValue(
      "incoming",
    );
    await page.getByLabel("Find host", { exact: true }).fill("report-a00.test");
    await page
      .locator(".network-matches")
      .getByRole("button", { name: "report-a00.test", exact: true })
      .click();
    await expect(page.locator(".network-hostname")).toHaveText(
      "report-a00.test",
    );
    await page.getByRole("button", { name: "Reset view", exact: true }).click();
    await expect
      .poll(() =>
        canvas.evaluate((c) => {
          const pixels = c
            .getContext("2d")
            .getImageData(0, 0, c.width, c.height).data;
          let blue = 0;
          for (let i = 0; i < pixels.length; i += 4)
            if (
              pixels[i] < 130 &&
              pixels[i + 1] > 150 &&
              pixels[i + 1] < 220 &&
              pixels[i + 2] > 220
            )
              blue++;
          return blue;
        }),
      )
      .toBeGreaterThan(10);
    await page
      .getByRole("button", { name: "Clear selection", exact: true })
      .click();
    await expect(page.locator(".network-hostname")).toHaveCount(0);
    await page
      .getByLabel("Find host", { exact: true })
      .fill("crawl-proxy.invalid");
    await page
      .locator(".network-matches")
      .getByRole("button", { name: "crawl-proxy.invalid", exact: true })
      .click();
    await expect(page.locator(".network-hostname")).toHaveText(
      "crawl-proxy.invalid",
    );
    await page
      .getByRole("button", { name: "Center host", exact: true })
      .click();
    await page.getByRole("button", { name: "Zoom in", exact: true }).click();
    await page.getByRole("button", { name: "Zoom out", exact: true }).click();
    await canvas.focus();
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("+");
    await page.keyboard.press("Home");
    await canvas.hover();
    await page.mouse.wheel(0, -250);
    await page.mouse.move(box.x + 100, box.y + 100);
    await page.mouse.down();
    await page.mouse.move(box.x + 160, box.y + 140, { steps: 4 });
    await page.mouse.up();
    await page.getByRole("button", { name: "Reset view", exact: true }).click();
    await expect(
      page.getByLabel("Visible node budget", { exact: true }),
    ).toHaveCount(0);
    await expect(
      page.getByLabel("Visible edge budget", { exact: true }),
    ).toHaveCount(0);
    const zoomed = await api(request, "atlas_view", {
      crawl_id: record.id,
      snapshot: status.snapshot.id,
      bounds: [
        chosen.x - 0.01,
        chosen.y - 0.01,
        chosen.x + 0.01,
        chosen.y + 0.01,
      ],
      node_limit: 50,
      edge_limit: 100,
    });
    for (const node of zoomed.nodes) {
      const original = frame.nodes.find((n) => n.host_id === node.host_id);
      expect(original).toBeTruthy();
      expect(node.x).toBe(original.x);
      expect(node.y).toBe(original.y);
      expect(node.community).toBe(original.community);
    }
    expect(await canvas.evaluate((c) => c._bufferResizes)).toBe(0);
    await page.reload();
    await expect(
      page.getByRole("status").filter({ hasText: /hosts in view/ }),
    ).toBeVisible();
    await page
      .getByLabel("Find host", { exact: true })
      .fill("crawl-proxy.invalid");
    await page
      .locator(".network-matches")
      .getByRole("button", { name: "crawl-proxy.invalid", exact: true })
      .click();
    await expect(page.locator(".network-hostname")).toHaveText(
      "crawl-proxy.invalid",
    );
    await page.getByRole("button", { name: "Reset view", exact: true }).click();
    await expect
      .poll(() =>
        canvas.evaluate((c) => {
          const data = c
            .getContext("2d")
            .getImageData(0, 0, c.width, c.height).data;
          let orange = 0;
          for (let i = 0; i < data.length; i += 4)
            if (
              data[i] > 220 &&
              data[i + 1] > 120 &&
              data[i + 1] < 200 &&
              data[i + 2] < 130
            )
              orange++;
          return orange;
        }),
      )
      .toBeGreaterThan(10);
    await page.screenshot({
      path: testInfo.outputPath("network.png"),
      fullPage: true,
    });
    await expect(page.getByRole("alert")).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.getByRole("button", { name: "Reports", exact: true }).click();
    await expect(page).toHaveURL(/\/report$/);
    expect(errors).toEqual([]);
  });

test("million-host network stays bounded through generation, cancellation and navigation", async ({
  page,
  request,
}, testInfo) => {
  test.setTimeout(480000);
  await createForm(page, await info(request), {
    name: `Million network ${crypto.randomUUID()}`,
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
  const seeded = await request.post(`${base}/fixture/atlas-seed`, {
    data: { crawl_id: record.id, count: 1000000 },
    timeout: 90000,
  });
  expect(seeded.ok(), await seeded.text()).toBe(true);
  let focusTiles = 0;
  const frames = [],
    errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("response", async (response) => {
    if (
      response.url().endsWith("/crawls/atlas_tile") &&
      (response.request().postDataJSON().selected_host || 0) > 0
    )
      focusTiles++;
    if (response.url().endsWith("/crawls/atlas_view")) {
      try {
        const b = await response.json();
        if (b.success)
          frames.push({
            nodes: b.data.nodes.length,
            edges: b.data.edges.length + (b.data.focus?.length || 0),
            focused: (response.request().postDataJSON().selected_host || 0) > 0,
          });
      } catch {
        /* aborted superseded viewport */
      }
    }
  });
  await page.goto(`${base}/crawls/${record.id}/network`);
  await page.getByText("Generation parameters", { exact: true }).click();
  await page.getByLabel("Host limit", { exact: true }).fill("2000000");
  await page.getByLabel("Edge limit", { exact: true }).fill("3000000");
  await page.getByLabel("Memory budget (MiB)", { exact: true }).fill("1536");
  await page.getByLabel("Analysis passes", { exact: true }).fill("4");
  await page.getByRole("button", { name: "Generate", exact: true }).click();
  await page
    .getByRole("button", { name: "Cancel generation", exact: true })
    .click();
  await expect(
    page.getByText("Generation cancelled", { exact: true }),
  ).toBeVisible({ timeout: 30000 });
  await page.getByRole("button", { name: "Generate", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Regenerate", exact: true }),
  ).toBeEnabled({ timeout: 300000 });
  const canvas = page.getByLabel("Interactive host network", { exact: true });
  await expect(canvas).toBeVisible();
  await expect(
    page.getByRole("status").filter({ hasText: /hosts in view/ }),
  ).toContainText("1,000,070", { timeout: 20000 });
  await page
    .getByLabel("Find host", { exact: true })
    .fill("scale-0500000.test");
  await page
    .locator(".network-matches")
    .getByRole("button", { name: "scale-0500000.test", exact: true })
    .click();
  await expect(page.locator(".network-hostname")).toHaveText(
    "scale-0500000.test",
  );
  await page.getByRole("button", { name: "Reset view", exact: true }).click();
  const beforeFocus = frames.length;
  await expect.poll(() => focusTiles).toBeGreaterThan(0);
  for (let i = 0; i < 4; i++)
    await page.getByRole("button", { name: "Zoom in", exact: true }).click();
  await expect.poll(() => frames.length).toBeGreaterThan(beforeFocus);
  await canvas.focus();
  await page.keyboard.press("ArrowLeft");
  await page.keyboard.press("ArrowDown");
  const beforeReset = frames.length;
  await page.getByRole("button", { name: "Reset view", exact: true }).click();
  await expect.poll(() => frames.length).toBeGreaterThan(beforeReset);
  await expect(
    page.getByRole("status").filter({ hasText: /hosts in view/ }),
  ).toContainText("1,000,070", { timeout: 20000 });
  await page
    .getByLabel("Find host", { exact: true })
    .fill("scale-0000000.test");
  await page
    .locator(".network-matches")
    .getByRole("button", { name: "scale-0000000.test", exact: true })
    .click();
  await expect(page.locator(".network-hostname")).toHaveText(
    "scale-0000000.test",
  );
  const firstNeighbor = await page
    .locator(".network-inspector li button")
    .first()
    .innerText();
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    page.locator(".network-inspector li button").first(),
  ).not.toHaveText(firstNeighbor);
  await page.getByRole("button", { name: "Previous", exact: true }).click();
  await expect(page.locator(".network-inspector li button").first()).toHaveText(
    firstNeighbor,
  );
  const session = await page.context().newCDPSession(page);
  await session.send("Performance.enable");
  const metrics = await session.send("Performance.getMetrics");
  const heap = metrics.metrics.find((m) => m.name === "JSHeapUsedSize").value;
  expect(heap).toBeLessThan(128 * 1024 * 1024);
  expect(frames.length).toBeGreaterThan(0);
  expect(frames.every((f) => f.nodes <= 2000 && f.edges <= 8000)).toBe(true);
  await testInfo.attach("million-browser-metrics", {
    body: JSON.stringify({ heap, frames }),
    contentType: "application/json",
  });
  await page.screenshot({
    path: testInfo.outputPath("million-network.png"),
    fullPage: true,
  });
  expect(errors).toEqual([]);
  await expect(page.getByRole("alert")).toHaveCount(0);
});

test("generation controls round-trip, reject invalid bounds, and preserve the previous snapshot", async ({
  page,
  request,
}) => {
  test.setTimeout(90000);
  await createForm(page, await info(request), {
    name: `Graph controls ${crypto.randomUUID()}`,
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
  await page.goto(`${base}/crawls/${record.id}/network`);
  await page.getByText("Generation parameters", { exact: true }).click();
  await page.getByLabel("Memory budget (MiB)", { exact: true }).fill("64");
  await page.getByLabel("Analysis passes", { exact: true }).fill("2");
  await page.getByLabel("Cluster size limit", { exact: true }).fill("20");
  let last = "";
  for (const visited of [false, true])
    for (const orphans of [false, true]) {
      await page
        .getByLabel("Visited hosts only", { exact: true })
        .setChecked(visited);
      await page
        .getByLabel("Exclude orphans", { exact: true })
        .setChecked(orphans);
      await page
        .getByRole("button", {
          name: last ? "Regenerate" : "Generate",
          exact: true,
        })
        .click();
      await expect
        .poll(
          async () =>
            (await api(request, "atlas_status", { crawl_id: record.id }))
              .snapshot?.id || "",
        )
        .not.toBe(last);
      const s = await api(request, "atlas_status", { crawl_id: record.id });
      last = s.snapshot.id;
      expect(s.snapshot.options).toMatchObject({
        visited_only: visited,
        remove_orphans: orphans,
        passes: 2,
        cluster_size: 20,
        memory_mb: 64,
      });
      await expect(
        page.getByRole("button", { name: "Regenerate", exact: true }),
      ).toBeEnabled();
    }
  for (const [label, invalid, valid] of [
    ["Host limit", "2000001", "200000"],
    ["Edge limit", "9", "1000000"],
    ["Memory budget (MiB)", "63", "64"],
    ["Analysis passes", "41", "2"],
    ["Cluster size limit", "19", "20"],
  ]) {
    const input = page.getByLabel(label, { exact: true });
    await input.fill(invalid);
    expect(await input.evaluate((i) => i.checkValidity())).toBe(false);
    await page.getByRole("button", { name: "Regenerate", exact: true }).click();
    expect(
      (await api(request, "atlas_status", { crawl_id: record.id })).snapshot.id,
    ).toBe(last);
    await input.fill(valid);
  }
  await page.reload();
  await page.getByText("Generation parameters", { exact: true }).click();
  await expect(
    page.getByLabel("Memory budget (MiB)", { exact: true }),
  ).toHaveValue("64");
  await expect(page.getByLabel("Analysis passes", { exact: true })).toHaveValue(
    "2",
  );
  await expect(
    page.getByLabel("Cluster size limit", { exact: true }),
  ).toHaveValue("20");
  await page.context().setOffline(true);
  await expect(page.getByRole("alert")).toBeVisible({ timeout: 15000 });
  await page.context().setOffline(false);
  await expect(page.getByRole("alert")).toHaveCount(0, { timeout: 15000 });
  await page.getByLabel("Host limit", { exact: true }).fill("10");
  await page.getByRole("button", { name: "Regenerate", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("host limit exceeded");
  expect(
    (await api(request, "atlas_status", { crawl_id: record.id })).snapshot.id,
  ).toBe(last);
  await expect(
    page.getByLabel("Interactive host network", { exact: true }),
  ).toBeVisible();
});

for (const mobile of [false, true]) {
  test(`two-host network remains legible ${mobile ? "mobile" : "desktop"}`, async ({
    page,
    request,
  }, testInfo) => {
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    await createForm(page, await info(request), {
      name: `Sparse network ${crypto.randomUUID()}`,
      strategy: "breadth_first",
      scope: "open_web",
      source: "urls",
      connection: "direct",
      drain: true,
      path: "network-small",
    });
    await page
      .getByRole("checkbox", { name: "Respect robots.txt", exact: true })
      .uncheck();
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    const record = await savedRecord(page, request);
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 45000,
    });
    await page
      .getByRole("navigation", { name: "Crawl views" })
      .getByRole("button", { name: "Graph", exact: true })
      .click();
    await page.getByText("Generation parameters", { exact: true }).click();
    for (const name of ["Visited hosts only", "Exclude orphans"]) {
      const toggle = page.getByRole("checkbox", { name, exact: true });
      const box = await toggle.boundingBox();
      expect(box.width).toBeLessThan(40);
    }
    await page.getByRole("button", { name: "Generate", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Regenerate", exact: true }),
    ).toBeEnabled();
    const status = await api(request, "atlas_status", { crawl_id: record.id });
    expect(status.snapshot.hosts).toBe(2);
    expect(status.snapshot.edges).toBe(2);
    const canvas = page.getByLabel("Interactive host network", { exact: true });
    const pixels = async () =>
      canvas.evaluate((c) => {
        const a = c.getContext("2d").getImageData(0, 0, c.width, c.height).data;
        const counts = { neutral: 0, incoming: 0, outgoing: 0 };
        for (let i = 0; i < a.length; i += 4) {
          const r = a[i],
            g = a[i + 1],
            b = a[i + 2];
          if (r > 15 && g > 25 && b > 35 && b < 160 && r < g && g < b)
            counts.neutral++;
          if (b > 80 && b > g + 15 && g > r + 10) counts.incoming++;
          if (r > 80 && r > g + 15 && g > b + 15) counts.outgoing++;
        }
        return counts;
      });
    // Count visible connecting pixels, not merely a canvas element or metadata.
    await expect
      .poll(async () => (await pixels()).neutral, { timeout: 20000 })
      .toBeGreaterThan(50);
    await page.getByLabel("Find host", { exact: true }).fill("localhost");
    await page
      .locator(".network-matches")
      .getByRole("button", { name: "localhost", exact: true })
      .click();
    await canvas.focus();
    await page.keyboard.press("Home");
    await expect
      .poll(async () => (await pixels()).incoming, { timeout: 20000 })
      .toBeGreaterThan(50);
    await expect
      .poll(async () => (await pixels()).outgoing, { timeout: 20000 })
      .toBeGreaterThan(50);
    await page.screenshot({
      path: testInfo.outputPath("sparse-network.png"),
      fullPage: true,
    });
  });
}
