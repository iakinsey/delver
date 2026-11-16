import { test, expect } from "./test";
import {
  base,
  api,
  info,
  createForm,
  savedRecord,
  cleanup,
} from "./helpers/workspace";
test.describe.configure({ mode: "parallel" });

for (const connection of ["direct", "http", "socks5"])
  for (const respect of [true, false])
    test(`robots respect=${respect} over ${connection} applies to pages and feeds`, async ({
      page,
      request,
    }) => {
      const fixture = await info(request);
      const agent = `RobotsToggle-${crypto.randomUUID()}`;
      await createForm(page, fixture, {
        name: agent,
        strategy: "host_diverse",
        scope: "open_web",
        source: "both",
        connection,
        drain: true,
        path: "robots-denied",
      });
      const checkbox = page.getByRole("checkbox", {
        name: "Respect robots.txt",
        exact: true,
      });
      await expect(checkbox).toBeChecked();
      await checkbox.setChecked(respect);
      await page.getByText("Requests", { exact: true }).click();
      await page.getByLabel("User-Agent", { exact: true }).fill(agent);
      await page
        .getByRole("button", {
          name: "Create",
          exact: true,
        })
        .click();
      const record = await savedRecord(page, request);
      try {
        expect(record.definition.network.respect_robots).toBe(respect);
        await page.getByRole("button", { name: "Edit", exact: true }).click();
        await expect(checkbox).toBeChecked({ checked: respect });
        await page.getByRole("button", { name: "Cancel", exact: true }).click();
        await page.getByRole("button", { name: "Start", exact: true }).click();
        await expect(page.locator(".crawl-state")).toHaveText("completed", {
          timeout: 20000,
        });
        const results = await api(request, "results", { crawl_id: record.id });
        const denied = results.rows.find(
          (row) => new URL(row.uri).pathname === "/robots-denied",
        );
        expect(denied).toBeTruthy();
        if (respect) expect(denied.error).toContain("robots disallows");
        else expect(denied.http_status).toBe(200);
        expect(
          results.rows.some(
            (row) =>
              new URL(row.uri).pathname === "/article" &&
              row.http_status === 200,
          ),
        ).toBe(true);
        const events = (
          await (await request.get(`${base}/fixture/request-events`)).json()
        ).filter((row) => row.user_agent === agent);
        expect(events.some((row) => row.path === "/feed")).toBe(true);
        expect(events.some((row) => row.path === "/robots-denied")).toBe(
          !respect,
        );
        expect(events.some((row) => row.path === "/robots.txt")).toBe(respect);
      } finally {
        await cleanup(request, record);
      }
    });
