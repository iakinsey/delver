import { test, expect } from "./test";
import { info, createForm, savedRecord, api } from "./helpers/workspace";
test.describe.configure({ mode: "parallel" });
for (const connection of ["direct", "http", "socks5", "https-proxy"])
  test(`verified TLS network path ${connection}`, async ({ page, request }) => {
    const fixture = await info(request);
    await createForm(page, fixture, {
      name: `TLS ${connection}`,
      strategy: "host_diverse",
      scope: "same_domain",
      source: "urls",
      connection: connection === "https-proxy" ? "http" : connection,
      drain: true,
    });
    if (connection === "https-proxy") {
      const proxy = new URL(fixture.https_proxy);
      proxy.username = fixture.proxy_username;
      proxy.password = fixture.proxy_password;
      await page.getByLabel("Proxy address", { exact: true }).fill(proxy.href);
    } else
      await page
        .getByLabel("Seed URLs", { exact: true })
        .fill(`${fixture.tls_source}/site`);
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    const record = await savedRecord(page, request);
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 20000,
    });
    const { rows } = await api(request, "results", { crawl_id: record.id });
    expect(rows).toHaveLength(2);
    expect(rows.every((r) => r.http_status === 200 && !r.error)).toBe(true);
    expect(
      rows.every(
        (r) =>
          new URL(r.uri).protocol ===
          (connection === "https-proxy" ? "http:" : "https:"),
      ),
    ).toBe(true);
  });
