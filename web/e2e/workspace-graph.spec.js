import { test, expect } from "./test";
import {
  info,
  createForm,
  savedRecord,
  api,
  scopes,
} from "./helpers/workspace";
test.describe.configure({ mode: "parallel" });
for (const scope of scopes)
  for (const depth of [1, 2])
    for (const hops of [1, 2])
      test(`graph ${scope} depth=${depth} hops=${hops}`, async ({
        page,
        request,
      }) => {
        test.setTimeout(45000);
        await createForm(page, await info(request), {
          name: `graph ${scope} ${depth} ${hops}`,
          strategy: "breadth_first",
          scope,
          source: "urls",
          connection: "http",
          drain: true,
          path: "graph",
        });
        await page.getByText("URL filters", { exact: true }).click();
        await page
          .getByLabel("Maximum depth", { exact: true })
          .fill(String(depth));
        await page
          .getByLabel("Maximum host hops", { exact: true })
          .fill(String(hops));
        await page
          .getByLabel("Allow pattern", { exact: true })
          .fill(
            "/(graph|child|grandchild|offsite|second-site|redirect|robots-denied|blocked|subdomain|escape|outside-redirect)",
          );
        await page.getByLabel("Deny pattern", { exact: true }).fill("/blocked");
        await page
          .getByRole("button", { name: "Create and start", exact: true })
          .click();
        const record = await savedRecord(page, request);
        await expect(page.locator(".crawl-state")).toHaveText("completed", {
          timeout: 25000,
        });
        const { rows } = await api(request, "results", { crawl_id: record.id });
        const paths = rows.map((r) => new URL(r.uri).pathname);
        expect(paths).toContain("/graph");
        expect(paths).toContain("/child");
        expect(paths).not.toContain("/blocked");
        expect(paths.includes("/grandchild")).toBe(depth === 2);
        expect(paths.includes("/subdomain")).toBe(scope !== "same_origin");
        const escaped = rows.find((r) => new URL(r.uri).pathname === "/escape");
        if (scope === "open_web") expect(escaped.http_status).toBe(200);
        else {
          expect(escaped.http_status).not.toBe(200);
          expect(escaped.error).toMatch(/outside|scope|boundar/);
        }
        expect(paths.includes("/offsite")).toBe(scope === "open_web");
        expect(paths.includes("/second-site")).toBe(
          scope === "open_web" && hops === 2,
        );
        expect(
          rows.find((r) => new URL(r.uri).pathname === "/redirect").http_status,
        ).toBe(200);
        const denied = rows.find(
          (r) => new URL(r.uri).pathname === "/robots-denied",
        );
        expect(denied.error).toMatch(/robots disallows/);
        expect(denied.http_status).not.toBe(200);
        await expect(
          page.getByRole("table").filter({
            has: page.getByRole("columnheader", {
              name: "Title or error",
              exact: true,
            }),
          }),
        ).toContainText("Graph root");
      });
