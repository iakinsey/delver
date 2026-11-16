import { test, expect } from "./test";
import { base } from "./helpers/workspace";
test("retired control surfaces do not create dashboards or streaming connections", async ({
  page,
  request,
}) => {
  const sockets = [];
  page.on("websocket", (socket) => sockets.push(socket.url()));
  for (const path of [
    "/crawler/status",
    "/crawler/draft",
    "/crawler/command",
    "/dashboard/list",
    "/dashboard/save",
  ]) {
    const response = await request.post(base + path, { data: {} });
    expect(response.status()).toBe(404);
  }
  await page.goto(base);
  await expect(
    page.getByRole("button", { name: "＋ Create crawl", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: /Runtime:/ })).toHaveCount(0);
  expect(sockets).toEqual([]);
});
