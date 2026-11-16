import { test, expect } from "./test";
import {
  base,
  info,
  createForm,
  savedRecord,
  api,
  scopes,
  scopeNames,
  cleanup,
  progress,
} from "./helpers/workspace";
test.describe.configure({ mode: "parallel" });
const basic = {
  strategy: "host_diverse",
  scope: "same_domain",
  source: "urls",
  connection: "direct",
  drain: false,
};
const invalid = [
  ["Name", "   "],
  ["Seed URLs", ""],
  ["Seed URLs", "ftp://example.com/file"],
  ["Seed URLs", "javascript:alert(1)"],
  ["Seed URLs", "https://user:private@example.com/"],
  ["Maximum depth", "0"],
  ["Maximum depth", "-1"],
  ["Maximum depth", "1.5"],
  ["Maximum host hops", "0"],
  ["Maximum host hops", "-1"],
  ["Per-host concurrency", "0"],
  ["Per-host concurrency", "3"],
  ["Per-host concurrency", "1.5"],
  ["Request delay", "-1s"],
  ["Request delay", "tomorrow"],
  ["Revisit interval", "0s"],
  ["Revisit interval", "-1s"],
  ["Revisit interval", "tomorrow"],
  ["Fetch limit", "-1"],
  ["Fetch limit", "1.5"],
  ["Time limit", "0s"],
  ["Time limit", "-1s"],
  ["Time limit", "tomorrow"],
  ["Completed records", "0d"],
  ["Completed records", "-7d"],
  ["Failed records", "0s"],
  ["URL deduplication", "1d"],
  ["URL deduplication", "forever"],
  ["Allow pattern", "["],
  ["Deny pattern", "("],
];
test("invalid guided inputs retain editor state", async ({ page, request }) => {
  await createForm(page, await info(request), {
    ...basic,
    name: "invalid settings",
  });
  for (const summary of ["URL filters", "Request pacing", "Run limits"])
    await page.getByText(summary, { exact: true }).click();
  for (const [field, value] of invalid) {
    const input = page.getByLabel(field, { exact: true }),
      previous = await input.inputValue();
    await input.fill(value);
    const response = page.waitForResponse(
      (r) =>
        r.url().endsWith("/crawls/create") && r.request().method() === "POST",
    );
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    expect(Boolean((await (await response).json()).success)).toBe(false);
    await expect(page.getByRole("alert")).toBeVisible();
    await expect(input).toHaveValue(value);
    await input.fill(previous);
  }
});
for (const connection of ["http", "socks5"])
  for (const value of ["", "ftp://proxy.invalid:21", "not a valid proxy"])
    test(`reject ${connection} proxy ${JSON.stringify(value)}`, async ({
      page,
      request,
    }) => {
      await createForm(page, await info(request), {
        ...basic,
        connection,
        name: `bad ${connection}`,
      });
      await page.getByLabel("Proxy address", { exact: true }).fill(value);
      await page
        .getByRole("button", { name: "Create and start", exact: true })
        .click();
      await expect(page.getByRole("alert")).toBeVisible();
      await expect(
        page.getByLabel("Proxy address", { exact: true }),
      ).toHaveValue(value);
    });
for (let mask = 0; mask < 8; mask++)
  test(`permanent retention combination ${mask.toString(2).padStart(3, "0")}`, async ({
    page,
    request,
  }) => {
    await createForm(page, await info(request), {
      ...basic,
      name: `retention ${mask}`,
    });
    const labels = ["Completed records", "Failed records", "URL deduplication"];
    for (const [i, label] of labels.entries())
      if (mask & (1 << i))
        await page
          .getByRole("checkbox", {
            name: `${label}: no expiry`,
            exact: true,
          })
          .check();
    await page.getByRole("button", { name: "Create", exact: true }).click();
    const valid = mask === 0 || !!(mask & 4);
    if (valid) {
      const record = await savedRecord(page, request);
      for (const [i, key] of [
        "completed_for",
        "failed_for",
        "dedup_for",
      ].entries())
        expect(record.definition.retention[key] === "never").toBe(
          !!(mask & (1 << i)),
        );
    } else {
      await expect(page.getByRole("alert")).toContainText(
        "dedup retention must cover detail retention",
      );
      await expect(
        page.getByRole("heading", { name: "Create crawl", exact: true }),
      ).toBeVisible();
    }
  });
for (const scope of scopes)
  test(`scope preview matrix ${scope}`, async ({ page, request }) => {
    await createForm(page, await info(request), {
      ...basic,
      scope,
      name: `preview ${scope}`,
    });
    await page
      .getByLabel("Seed URLs", { exact: true })
      .fill("https://docs.example.com/start");
    await page.getByText("URL filters", { exact: true }).click();
    for (const [target, sameOrigin, sameDomain] of [
      ["https://docs.example.com/page", true, true],
      ["https://help.example.com/page", false, true],
      ["https://outside.example.net/page", false, false],
      ["http://docs.example.com/page", false, true],
      ["https://docs.example.com:8443/page", false, true],
      ["ftp://docs.example.com/page", false, false],
    ]) {
      await page.getByLabel("Test URL", { exact: true }).fill(target);
      await page
        .getByRole("button", { name: "Check URL", exact: true })
        .click();
      const allowed =
        scope === "same_origin"
          ? sameOrigin
          : scope === "same_domain"
            ? sameDomain
            : !target.startsWith("ftp:");
      await expect(page.getByRole("status")).toContainText(
        allowed ? "Allowed" : "Excluded",
      );
    }
  });
test("preview invalidates old verdicts after rules change", async ({
  page,
  request,
}) => {
  const fixture = await info(request);
  await createForm(page, fixture, { ...basic, name: "changing preview" });
  await page.getByText("URL filters", { exact: true }).click();
  await page
    .getByLabel("Test URL", { exact: true })
    .fill(`${fixture.source}/site`);
  await page.getByRole("button", { name: "Check URL", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Allowed");
  await page.getByLabel("Deny pattern", { exact: true }).fill("/site");
  await expect(page.getByText(/Allowed these boundaries/)).toHaveCount(0);
  await page.getByRole("button", { name: "Check URL", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Excluded");
});
for (const concurrency of [1, 2])
  test(`zero-delay pacing and rapid retention concurrency ${concurrency}`, async ({
    page,
    request,
  }) => {
    const fixture = await info(request);
    await createForm(page, fixture, {
      ...basic,
      name: `retention pacing ${concurrency}`,
      revisit: "200ms",
    });
    await page.getByText("Request pacing", { exact: true }).click();
    await page.getByLabel("Request delay", { exact: true }).fill("0s");
    await page
      .getByLabel("Per-host concurrency", { exact: true })
      .fill(String(concurrency));
    for (const label of [
      "Completed records",
      "Failed records",
      "URL deduplication",
    ])
      await page.getByLabel(label, { exact: true }).fill("100ms");
    await page
      .getByRole("button", { name: "Create and start", exact: true })
      .click();
    const record = await savedRecord(page, request);
    try {
      await expect
        .poll(() => progress(page, "Processed"), { timeout: 15000 })
        .toBeGreaterThan(2);
      await page.getByRole("button", { name: "Pause", exact: true }).click();
      await expect(page.locator(".crawl-state")).toHaveText("paused", {
        timeout: 15000,
      });
    } finally {
      await cleanup(request, record);
    }
  });

for (const scope of ["same_origin", "same_domain"])
  test(`feed-only ${scope} preview explains unresolved boundaries`, async ({
    page,
    request,
  }) => {
    const fixture = await info(request);
    await createForm(page, fixture, {
      ...basic,
      source: "feeds",
      scope,
      name: `feed preview ${scope}`,
    });
    await page.getByText("URL filters", { exact: true }).click();
    await page
      .getByLabel("Test URL", { exact: true })
      .fill(`${fixture.source}/article`);
    await page.getByRole("button", { name: "Check URL", exact: true }).click();
    await expect(page.getByRole("status")).toContainText(
      "Scope unresolved: feed sources not loaded",
    );
  });

test("late preview responses cannot replace changed rules", async ({
  page,
  request,
}) => {
  const fixture = await info(request);
  await createForm(page, fixture, { ...basic, name: "late preview" });
  await page.getByText("URL filters", { exact: true }).click();
  await page
    .getByLabel("Test URL", { exact: true })
    .fill(`${fixture.source}/site`);
  let release,
    started = false,
    finished = false;
  const pending = new Promise((resolve) => (release = resolve));
  await page.route("**/crawls/preview", async (route) => {
    const response = await route.fetch();
    started = true;
    await pending;
    await route.fulfill({ response });
    finished = true;
  });
  await page.getByRole("button", { name: "Check URL", exact: true }).click();
  await expect.poll(() => started).toBe(true);
  await page.getByLabel("Deny pattern", { exact: true }).fill("/site");
  release();
  await expect.poll(() => finished).toBe(true);
  await expect(page.getByText(/Allowed these boundaries/)).toHaveCount(0);
});
