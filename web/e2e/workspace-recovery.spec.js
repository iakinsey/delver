import { test, expect } from "./test";

test("workspace reconnects without hiding failed form validation", async ({
  page,
}) => {
  let disconnected = false,
    listCalls = 0;
  await page.route("**/crawls/list", async (route) => {
    listCalls++;
    if (disconnected) await route.abort("connectionrefused");
    else await route.continue();
  });
  await page.goto(process.env.DELVER_E2E_BASE_URL);
  await expect(
    page.getByRole("button", { name: "＋ Create crawl", exact: true }),
  ).toBeVisible();
  disconnected = true;
  await expect(page.getByRole("alert")).toContainText("Server unavailable", {
    timeout: 10000,
  });
  disconnected = false;
  await expect(page.getByRole("alert")).toHaveCount(0, { timeout: 10000 });
  await page
    .getByRole("button", { name: "＋ Create crawl", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Create and start", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "at least one seed URL or feed",
  );
  // A successful background refresh must preserve the actionable validation error.
  const beforeRefresh = listCalls;
  await expect
    .poll(() => listCalls, { timeout: 10000 })
    .toBeGreaterThan(beforeRefresh);
  await expect(page.getByRole("alert")).toContainText(
    "at least one seed URL or feed",
  );
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveCount(0);
});
