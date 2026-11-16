import { test, expect } from "./test";
const workspace = process.env.DELVER_E2E_BASE_URL;

for (const path of ["/", "/dashboards", "/dashboard/old-saved-id", "/help"]) {
  test(`crawl workspace replaces the retired builder at ${path}`, async ({
    page,
  }) => {
    const dashboardRequests = [],
      sockets = [];
    page.on("request", (request) => {
      if (/\/dashboard\/(list|load|save)(?:\?|$)/.test(request.url()))
        dashboardRequests.push(request.url());
    });
    page.on("websocket", (socket) => sockets.push(socket.url()));
    const response = await page.goto(`${workspace}${path}`);
    if (path.startsWith("/dashboard/")) {
      expect(response.status()).toBe(404);
      expect(dashboardRequests).toEqual([]);
      expect(sockets).toEqual([]);
      return;
    }
    await expect(page).toHaveURL(`${workspace}/`);
    await expect(
      page.getByRole("button", { name: "＋ Create crawl", exact: true }),
    ).toBeVisible();
    await expect(page.getByRole("combobox", { name: "Settings" })).toHaveCount(
      0,
    );
    await expect(
      page.getByRole("button", { name: /Create dashboard|Save dashboard/ }),
    ).toHaveCount(0);
    expect(dashboardRequests).toEqual([]);
    expect(sockets).toEqual([]);
  });
}

test("workspace exposes no legacy single-crawl controls", async ({ page }) => {
  await page.goto("/");
  await expect(
    page.getByRole("button", { name: "＋ Create crawl", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Settings" })).toHaveCount(0);
});
