import { test, expect } from "./test";
import {
  base,
  api,
  info,
  createForm,
  savedRecord,
  cleanup,
} from "./helpers/workspace";
test("lost stop response does not duplicate the executed operation", async ({
  page,
  request,
}) => {
  let record;
  try {
    await createForm(page, await info(request), {
      name: "lost stop",
      strategy: "host_diverse",
      scope: "same_origin",
      source: "urls",
      connection: "direct",
      drain: false,
    });
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    record = await savedRecord(page, request);
    await expect(page.locator(".crawl-state")).toHaveText("running");
    await page.route(
      "**/crawls/command",
      async (route) => {
        await route.fetch();
        await route.abort("failed");
      },
      { times: 1 },
    );
    await page.getByRole("button", { name: "Stop", exact: true }).click();
    await expect(
      page.getByRole("alert").filter({ hasText: "Server unavailable" }),
    ).toBeVisible();
    await expect(page.locator(".crawl-state")).toHaveText("stopped");
    const operations = await api(request, "operations", {
      crawl_id: record.id,
    });
    expect(
      Object.values(operations).filter((op) => op.action === "stop"),
    ).toHaveLength(1);
  } finally {
    if (record) await cleanup(request, record);
  }
});
