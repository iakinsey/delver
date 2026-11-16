import { test, expect } from "./test";
import AxeBuilder from "@axe-core/playwright";

test("crawl workspace has accessible labels and contrast", async ({ page }) => {
  await page.goto(process.env.DELVER_E2E_BASE_URL);
  await expect(
    page.getByRole("button", { name: "＋ Create crawl", exact: true }),
  ).toBeVisible();
  const result = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa"])
    .analyze();
  expect(result.violations).toEqual([]);
});
