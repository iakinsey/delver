import { test, expect } from "./test";
import { base, api, info, createForm, savedRecord } from "./helpers/workspace";
test.describe.configure({ mode: "parallel" });
const basic = {
  strategy: "host_diverse",
  scope: "same_domain",
  source: "urls",
  connection: "direct",
  drain: true,
};
for (const action of ["create", "start_run"])
  test(`lost ${action} response does not duplicate a UI-created run`, async ({
    page,
    request,
  }) => {
    const name = `lost ${action} ${crypto.randomUUID()}`;
    await createForm(page, await info(request), { ...basic, name });
    let first = true;
    const submitted = [];
    await page.route(`**/crawls/${action}`, async (route) => {
      submitted.push(route.request().postDataJSON());
      if (first) {
        first = false;
        await route.fetch();
        await route.abort("connectionrefused");
      } else await route.continue();
    });
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    await expect(page.getByRole("alert")).toBeVisible();
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    const record = await savedRecord(page, request);
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 20000,
    });
    expect(
      (await api(request, "detail", { crawl_id: record.id })).runs,
    ).toHaveLength(1);
    expect(
      (await api(request, "list")).filter((c) => c.definition.name === name),
    ).toHaveLength(1);
    expect(submitted).toHaveLength(2);
    expect(submitted[1].id).toBe(submitted[0].id);
    if (action === "start_run")
      expect(submitted[1].revision).toBe(submitted[0].revision);
  });
for (const action of ["draft", "activate"])
  test(`lost ${action} response safely retries save-and-start replacement`, async ({
    page,
    request,
  }) => {
    await createForm(page, await info(request), {
      ...basic,
      name: `before ${action}`,
    });
    await page.getByRole("button", { name: "Create", exact: true }).click();
    const record = await savedRecord(page, request);
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page.getByLabel("Name", { exact: true }).fill(`after ${action}`);
    let first = true;
    const submitted = [];
    await page.route(`**/crawls/${action}`, async (route) => {
      submitted.push(route.request().postDataJSON());
      if (first) {
        first = false;
        await route.fetch();
        await route.abort("connectionrefused");
      } else await route.continue();
    });
    await page
      .getByRole("button", {
        name: "Save and restart",
        exact: true,
      })
      .click();
    await expect(page.getByRole("alert")).toBeVisible();
    await page
      .getByRole("button", {
        name: "Save and restart",
        exact: true,
      })
      .click();
    await expect(
      page.getByRole("heading", { name: `after ${action}`, exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Edit this crawl", exact: true }),
    ).toHaveCount(0);
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 20000,
    });
    expect(
      (await api(request, "detail", { crawl_id: record.id })).runs,
    ).toHaveLength(1);
    expect(submitted).toHaveLength(2);
    expect(submitted[1]).toEqual(submitted[0]);
  });
