import { test, expect } from "./test";
import { info, createForm, savedRecord, api } from "./helpers/workspace";

test.describe.configure({ mode: "parallel" });
for (const connection of ["direct", "http", "socks5", "https-proxy"])
  for (const scheme of ["http", "https"])
    for (const framing of ["fixed", "gzip", "chunked"])
      test(`protected body ${connection} ${scheme} ${framing}`, async ({
        page,
        request,
      }) => {
        const fixture = await info(request);
        await createForm(page, fixture, {
          name: `Protected ${connection} ${scheme} ${framing} ${crypto.randomUUID()}`,
          strategy: "breadth_first",
          scope: "open_web",
          source: "urls",
          connection: connection === "https-proxy" ? "http" : connection,
          drain: true,
        });
        if (connection === "https-proxy") {
          const proxy = new URL(fixture.https_proxy);
          proxy.username = fixture.proxy_username;
          proxy.password = fixture.proxy_password;
          await page
            .getByLabel("Proxy address", { exact: true })
            .fill(proxy.href);
        }
        await page
          .getByLabel("Seed URLs", { exact: true })
          .fill(
            `${scheme === "https" ? fixture.tls_source : fixture.source}/body-${framing}`,
          );
        await page
          .getByRole("button", { name: "Create and start", exact: true })
          .click();
        const record = await savedRecord(page, request);
        await expect(page.locator(".crawl-state")).toHaveText("completed", {
          timeout: 30000,
        });
        const detail = await api(request, "detail", { crawl_id: record.id });
        const metrics = detail.runs.at(-1).summary.fetch;
        expect(metrics.totals.successes).toBe(1);
        expect(metrics.totals.bytes_received).toBe(131072);
        expect(metrics.totals.bytes_stored).toBe(131072);
        expect(metrics.network_active).toBe(0);
        expect(metrics.active).toBe(0);
        if (framing === "fixed") {
          expect(metrics.counters.body_mapped).toBe(1);
          expect(metrics.counters.object_splice_bytes).toBe(131072);
        } else {
          expect(metrics.counters.body_buffered).toBe(1);
          expect(metrics.counters.object_splice_bytes || 0).toBe(0);
        }
        await page.getByText("Details", { exact: true }).first().click();
        await expect(
          page.getByText(
            `${framing === "fixed" ? "body_mapped" : "body_buffered"}: 1`,
            { exact: true },
          ),
        ).toBeVisible();
      });
