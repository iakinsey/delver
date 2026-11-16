import { expect } from "../test";
export const base = process.env.DELVER_E2E_BASE_URL;
export const strategies = [
  "host_diverse",
  "breadth_first",
  "depth_first",
  "adaptive",
];
export const scopes = ["same_origin", "same_domain", "open_web"];
export const sources = ["urls", "feeds", "both"];
export const connections = ["direct", "http", "socks5"];
export const strategyNames = [
  /^Host-diverse/,
  /^Breadth-first/,
  /^Depth-first/,
  /^Adaptive Prioritize/,
];
export const scopeNames = [/^Same host$/, /^Same domain/, /^Open web$/];
export const connectionNames = [/^Direct$/, /^HTTP \/ HTTPS$/, /^SOCKS5$/];
export async function api(request, action, data = {}) {
  const response = await request.post(`${base}/crawls/${action}`, { data });
  const body = await response.json();
  expect(body.success, body.error).toBe(true);
  return body.data;
}
export async function info(request) {
  return (await request.get(`${base}/fixture/info`)).json();
}
export async function createForm(page, fixture, options) {
  await page.goto(base);
  await page
    .getByRole("button", { name: "＋ Create crawl", exact: true })
    .click();
  await page.getByLabel("Name", { exact: true }).fill(options.name);
  const source =
    options.connection === "direct"
      ? fixture.source
      : fixture.source.replace("127.0.0.1", "crawl-proxy.invalid");
  if (options.source !== "feeds")
    await page
      .getByLabel("Seed URLs", { exact: true })
      .fill(`${source}/${options.path || "site"}`);
  if (options.source !== "urls") {
    await page.getByText("RSS / Atom feeds", { exact: true }).click();
    await page.getByLabel("Feed URLs", { exact: true }).fill(`${source}/feed`);
  }
  await page
    .getByRole("radio", {
      name: strategyNames[strategies.indexOf(options.strategy)],
    })
    .check();
  await page
    .getByRole("radio", { name: scopeNames[scopes.indexOf(options.scope)] })
    .check();
  await page
    .getByRole("radio", {
      name: options.drain ? /^Drain Read/ : /^Monitor Scheduled/,
    })
    .check();
  if (!options.drain)
    await page
      .getByLabel("Revisit interval", { exact: true })
      .fill(options.revisit || "2s");
  await page
    .getByRole("radio", {
      name: connectionNames[connections.indexOf(options.connection)],
    })
    .check();
  if (options.connection !== "direct") {
    let proxy = fixture.socks;
    if (options.connection === "http") {
      proxy = new URL(fixture.proxy);
      proxy.username = fixture.proxy_username;
      proxy.password = fixture.proxy_password;
      proxy = proxy.href;
    }
    await page.getByLabel("Proxy address", { exact: true }).fill(proxy);
  }
  return source;
}
export async function savedRecord(page, request) {
  await expect(page).toHaveURL(/\/crawls\/[a-f0-9-]+$/);
  return api(request, "detail", {
    crawl_id: new URL(page.url()).pathname.split("/")[2],
  });
}
export async function progress(page, label) {
  return Number(
    await page
      .getByLabel("Crawl progress", { exact: true })
      .locator("div")
      .filter({ has: page.getByText(label, { exact: true }) })
      .locator("strong")
      .innerText(),
  );
}
export async function cleanup(request, record) {
  // Close active runtimes even when an assertion fails; archived data remains
  // inspectable until the isolated fixture process exits.
  const detail = await api(request, "detail", { crawl_id: record.id });
  if (
    ["running", "paused", "pausing", "draining"].includes(detail.runtime?.state)
  ) {
    await api(request, "command", {
      crawl_id: record.id,
      action: "stop",
      id: crypto.randomUUID(),
    });
    await expect
      .poll(
        async () =>
          (await api(request, "detail", { crawl_id: record.id })).runtime
            ?.state,
        { timeout: 20000 },
      )
      .toBe("stopped");
  }
}
