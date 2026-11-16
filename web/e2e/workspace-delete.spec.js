import { test, expect } from "./test";
import { base, api, info, createForm, savedRecord } from "./helpers/workspace";
test.describe.configure({ mode: "parallel" });
for (const state of ["draft", "running", "archived"])
  for (const preserve of [true, false])
    test(`delete ${state} crawl preserve logs=${preserve} requires confirmation`, async ({
      page,
      request,
    }) => {
      const fixture = await info(request);
      await createForm(page, fixture, {
        name: `delete ${state} ${crypto.randomUUID()}`,
        strategy: "host_diverse",
        scope: "same_origin",
        source: "urls",
        connection: "direct",
        drain: false,
      });
      await page
        .getByRole("button", {
          name: "Create",
          exact: true,
        })
        .click();
      const record = await savedRecord(page, request);
      if (state === "running") {
        await page.getByRole("button", { name: "Start", exact: true }).click();
        await expect(page.locator(".crawl-state")).toHaveText("running");
      }
      if (state === "archived") {
        await page
          .getByRole("button", { name: "Archive", exact: true })
          .click();
        await expect(
          page.getByRole("button", { name: "Restore", exact: true }),
        ).toBeVisible();
      }
      await page.getByRole("button", { name: "Delete", exact: true }).click();
      const dialog = page.getByRole("alertdialog");
      await expect(dialog).toBeVisible();
      await expect(dialog).toContainText("Delete saved data and run history");
      await expect(
        dialog.getByRole("checkbox", {
          name: "Preserve crawl logs",
          exact: true,
        }),
      ).toBeChecked();
      await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
      await expect(
        page.getByRole("button", { name: "Delete", exact: true }),
      ).toBeFocused();
      expect((await api(request, "detail", { crawl_id: record.id })).id).toBe(
        record.id,
      );
      await page.getByRole("button", { name: "Delete", exact: true }).click();
      await dialog
        .getByRole("checkbox", { name: "Preserve crawl logs", exact: true })
        .setChecked(preserve);
      const sent = page.waitForRequest((r) =>
        r.url().endsWith("/crawls/delete"),
      );
      await dialog.getByRole("button", { name: "Delete", exact: true }).click();
      expect((await sent).postDataJSON()).toMatchObject({
        crawl_id: record.id,
        confirm: true,
        preserve_logs: preserve,
      });
      await expect(page).toHaveURL(`${base}/`, { timeout: 15000 });
      expect((await api(request, "list")).some((r) => r.id === record.id)).toBe(
        false,
      );
      await page.reload();
      expect((await api(request, "list")).some((r) => r.id === record.id)).toBe(
        false,
      );
    });

test("API rejects unconfirmed deletion", async ({ request }) => {
  const d = await api(request, "defaults");
  d.name = "confirmation required";
  d.seeds = ["http://example.test/"];
  const record = await api(request, "create", { definition: d });
  const response = await request.post(`${base}/crawls/delete`, {
    data: { crawl_id: record.id, revision: record.revision },
  });
  expect((await response.json()).success).toBeFalsy();
  expect((await api(request, "detail", { crawl_id: record.id })).id).toBe(
    record.id,
  );
});

test("accepted deletion survives a lost response", async ({
  page,
  request,
}) => {
  const definition = await api(request, "defaults");
  definition.name = "delete response loss";
  definition.seeds = ["http://example.test/"];
  const record = await api(request, "create", { definition });
  await page.goto(`${base}/crawls/${record.id}`);
  await page.route("**/crawls/delete", async (route) => {
    await route.fetch();
    await route.abort();
  });
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await page
    .getByRole("alertdialog")
    .getByRole("button", { name: "Delete", exact: true })
    .click();
  await expect(page).toHaveURL(`${base}/`, { timeout: 15000 });
  expect((await api(request, "list")).some((c) => c.id === record.id)).toBe(
    false,
  );
});
