import { test, expect } from "./test";
import {
  base,
  api,
  info,
  createForm,
  savedRecord,
  cleanup,
  progress,
} from "./helpers/workspace";
test.describe.configure({ mode: "parallel" });
const basic = {
  strategy: "host_diverse",
  scope: "same_domain",
  source: "urls",
  connection: "direct",
  drain: false,
};
test("archiving one live crawl preserves another and restore does not start it", async ({
  page,
  request,
}) => {
  test.setTimeout(60000);
  const fixture = await info(request);
  await createForm(page, fixture, { ...basic, name: "archive live" });
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  const first = await savedRecord(page, request);
  let second;
  try {
    await expect(page.locator(".crawl-state")).toHaveText("running", {
      timeout: 15000,
    });
    await page.getByRole("button", { name: "Duplicate", exact: true }).click();
    await expect(page).not.toHaveURL(`${base}/crawls/${first.id}`);
    second = await savedRecord(page, request);
    await expect(page.locator(".crawl-state")).toHaveText("not started");
    expect(second.runs).toHaveLength(0);
    await page.getByRole("button", { name: "Start", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("running", {
      timeout: 15000,
    });
    await page.getByRole("button", { name: "Archive", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Restore", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Resume", exact: true }),
    ).toHaveCount(0);
    expect(
      (await api(request, "detail", { crawl_id: first.id })).runtime.state,
    ).toBe("running");
    await page.getByRole("link", { name: "Delver", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "archive live copy", exact: true }),
    ).toHaveCount(0);
    await page
      .getByRole("checkbox", { name: "Show archived", exact: true })
      .check();
    await page
      .getByRole("button", { name: "archive live copy", exact: true })
      .click();
    await page.getByRole("button", { name: "Restore", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("stopped");
    await page.getByRole("button", { name: "Resume", exact: true }).click();
    await expect(page.locator(".crawl-state")).toHaveText("running", {
      timeout: 15000,
    });
  } finally {
    await cleanup(request, first);
    if (second) await cleanup(request, second);
  }
});
test("saving a draft leaves a live run unchanged and applying retains its state", async ({
  page,
  request,
}) => {
  test.setTimeout(60000);
  const fixture = await info(request);
  await createForm(page, fixture, { ...basic, name: "live draft history" });
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  const record = await savedRecord(page, request);
  try {
    await expect
      .poll(() => progress(page, "Processed"), { timeout: 15000 })
      .toBeGreaterThan(0);
    const original = await api(request, "detail", { crawl_id: record.id });
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page
      .getByLabel("Name", { exact: true })
      .fill("replacement finite history");
    await page
      .getByLabel("Seed URLs", { exact: true })
      .fill(`${fixture.source}/article`);
    await page.getByRole("radio", { name: /^Drain Read/ }).check();
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "live draft history", exact: true }),
    ).toBeVisible();
    const active = await api(request, "detail", { crawl_id: record.id });
    expect(active.runs.at(-1).id).toBe(original.runs.at(-1).id);
    expect(active.runtime.state).toBe("running");
    await page.reload();
    await expect(
      page.getByRole("button", {
        name: "Apply and restart",
        exact: true,
      }),
    ).toBeVisible();
    await page
      .getByRole("button", {
        name: "Apply and restart",
        exact: true,
      })
      .click();
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 20000,
    });
    await expect(
      page.getByRole("heading", {
        name: "replacement finite history",
        exact: true,
      }),
    ).toBeVisible();
    expect(
      (await api(request, "detail", { crawl_id: record.id })).runs,
    ).toHaveLength(1);
    const recent = page.getByRole("table").filter({
      has: page.getByRole("columnheader", {
        name: "Title or error",
        exact: true,
      }),
    });
    await expect(recent).toContainText("/article");
    await expect(recent).toContainText("Workspace site");
    const retained = await api(request, "detail", { crawl_id: record.id });
    expect(retained.runs[0].id).toBe(original.runs[0].id);
    await page.getByRole("button", { name: "View", exact: true }).click();
    await expect(page.getByText("Run:", { exact: false })).toBeVisible();
    await expect(recent).toContainText("Workspace site");
    await page.getByRole("button", { name: "Latest", exact: true }).click();
    await expect(recent).toContainText("Workspace site");
  } finally {
    await cleanup(request, record);
  }
});
test("proxy changes preserve secrets within a mode and clear them on direct", async ({
  page,
  request,
}) => {
  test.setTimeout(45000);
  const fixture = await info(request);
  await createForm(page, fixture, {
    ...basic,
    name: "proxy transitions",
    connection: "http",
    drain: true,
  });
  await page.getByRole("button", { name: "Create", exact: true }).click();
  const record = await savedRecord(page, request);
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(page.getByLabel("Proxy address", { exact: true })).toHaveValue(
    /browser-proxy-secret/,
  );
  await page.getByRole("radio", { name: /^Direct$/ }).check();
  await page
    .getByLabel("Seed URLs", { exact: true })
    .fill(`${fixture.source}/site`);
  await expect(page.getByLabel("Proxy address", { exact: true })).toHaveCount(
    0,
  );
  await page
    .getByRole("button", {
      name: "Save and restart",
      exact: true,
    })
    .click();
  await expect(
    page.getByRole("button", { name: "Edit", exact: true }),
  ).toBeEnabled({ timeout: 15000 });
  await expect(page.locator(".crawl-state")).toHaveText("completed", {
    timeout: 20000,
  });
  const direct = await api(request, "detail", { crawl_id: record.id });
  expect(direct.definition.proxy).toEqual({ mode: "direct", url: "" });
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await page.getByRole("radio", { name: /^SOCKS5$/ }).check();
  await page
    .getByLabel("Seed URLs", { exact: true })
    .fill(`${fixture.source.replace("127.0.0.1", "crawl-proxy.invalid")}/site`);
  await page.getByLabel("Proxy address", { exact: true }).fill(fixture.socks);
  await page
    .getByRole("button", {
      name: "Save and restart",
      exact: true,
    })
    .click();
  await expect(
    page.getByRole("button", { name: "Edit", exact: true }),
  ).toBeEnabled({ timeout: 15000 });
  await expect(page.locator(".crawl-state")).toHaveText("completed", {
    timeout: 20000,
  });
  expect(await progress(page, "Failed fetches")).toBe(0);
  expect(
    JSON.stringify(await api(request, "detail", { crawl_id: record.id })),
  ).toContain(fixture.proxy_password);
});
