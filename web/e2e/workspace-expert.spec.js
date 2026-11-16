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
async function expert(page, request, options = {}) {
  await createForm(page, await info(request), {
    ...basic,
    name: "expert controls",
    ...options,
  });
  await page.getByRole("button", { name: "Create", exact: true }).click();
  const record = await savedRecord(page, request);
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await page.getByText("Worker pipeline", { exact: true }).click();
  await page
    .getByRole("button", {
      name: "Edit pipeline JSON",
      exact: true,
    })
    .click();
  const editor = page.getByLabel("Worker pipeline JSON", { exact: true });
  await expect(editor).toBeVisible();
  return { record, editor, pipeline: JSON.parse(await editor.inputValue()) };
}
test("expert syntax errors retain text and recovery preserves opaque integer parameters", async ({
  page,
  request,
}) => {
  const { record, editor, pipeline } = await expert(page, request);
  for (const name of ["", "   "]) {
    await page.getByLabel("Name", { exact: true }).fill(name);
    await page
      .getByRole("button", {
        name: "Save and restart",
        exact: true,
      })
      .click();
    await expect(page.getByRole("alert")).toContainText("name required");
  }
  await page.getByLabel("Name", { exact: true }).fill("expert controls");
  pipeline.workers.find((w) => w.name === "save").type = "fixture_failure";
  pipeline.workers.find((w) => w.name === "save").parameters = {
    token: "opaque-parameters",
    opaque: "LARGE_NUMBER",
    nested: { preserve: [true, false, "value"] },
  };
  const text = JSON.stringify(pipeline, null, 2).replace(
    '"LARGE_NUMBER"',
    "9007199254740993123",
  );
  await editor.fill('{"private":"must-not-echo",');
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveText("Invalid pipeline JSON");
  await expect(editor).toHaveValue('{"private":"must-not-echo",');
  await editor.fill(text);
  await page.getByLabel("Name", { exact: true }).fill("opaque custom pipeline");
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(page.getByText(/Draft saved/)).toBeVisible();
  expect(
    (await api(request, "pipeline", { crawl_id: record.id }))
      .configuration_json,
  ).toContain("9007199254740993123");
  await page.reload();
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(
    page.getByLabel("Worker pipeline JSON", { exact: true }),
  ).toContainText("9007199254740993123");
  await expect(page.getByLabel("Seed URLs", { exact: true })).toHaveCount(0);
});
test("expert finite completion rejects recurring sources without losing edits", async ({
  page,
  request,
}) => {
  await expert(page, request, { source: "feeds", drain: false });
  await page.getByRole("radio", { name: /Drain Requires/ }).check();
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "source jobs to declare once:true",
  );
  await expect(
    page.getByLabel("Worker pipeline JSON", { exact: true }),
  ).toBeVisible();
});
test("expert request budget rejects a pipeline without a frontier", async ({
  page,
  request,
}) => {
  const { editor, pipeline } = await expert(page, request);
  pipeline.resources = pipeline.resources.filter(
    (r) => r.type !== "frontier_store",
  );
  pipeline.workers = pipeline.workers.filter(
    (w) => !w.type.startsWith("frontier_"),
  );
  pipeline.workers.find((w) => w.name === "starting_urls").outbox = "requests";
  await editor.fill(JSON.stringify(pipeline, null, 2));
  await page.getByLabel("Fetch limit", { exact: true }).fill("1");
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "exactly one frontier_store",
  );
});
for (const connection of ["http", "socks5"])
  test(`expert conversion retains operational ${connection} secrets`, async ({
    page,
    request,
  }) => {
    const { record, editor } = await expert(page, request, { connection });
    await expect(editor).toHaveValue(/browser-proxy-secret/);
    await page
      .getByRole("button", {
        name: "Save and restart",
        exact: true,
      })
      .click();
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 20000,
    });
    const { rows } = await api(request, "results", { crawl_id: record.id });
    expect(rows.length).toBeGreaterThan(0);
    expect(rows.every((r) => r.http_status === 200)).toBe(true);
  });
test("two editors reject stale saves while preserving both entered text and winning draft", async ({
  page,
  request,
}) => {
  await createForm(page, await info(request), {
    ...basic,
    name: "conflict original",
  });
  await page.getByRole("button", { name: "Create", exact: true }).click();
  const record = await savedRecord(page, request);
  const other = await page.context().newPage();
  try {
    await other.goto(page.url());
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await other.getByRole("button", { name: "Edit", exact: true }).click();
    await page.getByLabel("Name", { exact: true }).fill("winning draft");
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await expect(page.getByText(/Draft saved/)).toBeVisible();
    await other.getByLabel("Name", { exact: true }).fill("stale draft");
    await other
      .getByRole("button", { name: "Save draft", exact: true })
      .click();
    await expect(other.getByRole("alert")).toContainText("revision conflict");
    await expect(other.getByLabel("Name", { exact: true })).toHaveValue(
      "stale draft",
    );
    expect(
      (await api(request, "detail", { crawl_id: record.id })).draft.name,
    ).toBe("winning draft");
    await other.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(other.getByRole("alert")).toHaveCount(0);
  } finally {
    await other.close();
  }
});

test("initialization failure is visible and a corrected pipeline starts independently", async ({
  page,
  request,
}) => {
  test.setTimeout(60000);
  const fixture = await info(request),
    defaults = await api(request, "defaults");
  const healthy = await api(request, "create", {
    definition: {
      ...defaults,
      name: "healthy beside failed init",
      seeds: [`${fixture.source}/site`],
    },
  });
  await api(request, "start_run", {
    crawl_id: healthy.id,
    revision: healthy.revision,
  });
  const { record, editor, pipeline } = await expert(page, request, {
    name: "initialization failure",
  });
  try {
    const sink = pipeline.workers.find((w) => w.name === "save");
    sink.type = "fixture_failure";
    sink.parameters = { token: "" };
    await editor.fill(JSON.stringify(pipeline, null, 2));
    await page
      .getByRole("button", {
        name: "Save and restart",
        exact: true,
      })
      .click();
    await expect(page.locator(".crawl-state")).toHaveText("failed", {
      timeout: 15000,
    });
    await expect(
      page.getByText(/fixture failure token required/).first(),
    ).toBeVisible();
    expect(
      (await api(request, "detail", { crawl_id: healthy.id })).runtime.state,
    ).toBe("running");
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    const corrected = JSON.parse(
      await page
        .getByLabel("Worker pipeline JSON", { exact: true })
        .inputValue(),
    );
    const correctedSink = corrected.workers.find((w) => w.name === "save");
    correctedSink.type = "jsonl_sink";
    correctedSink.parameters = { path: "recovered-results.jsonl" };
    await page
      .getByLabel("Worker pipeline JSON", { exact: true })
      .fill(JSON.stringify(corrected, null, 2));
    await page
      .getByRole("button", {
        name: "Save and restart",
        exact: true,
      })
      .click();
    await expect(page.locator(".crawl-state")).toHaveText("completed", {
      timeout: 20000,
    });
    expect(
      (await api(request, "results", { crawl_id: record.id })).rows.some(
        (r) => r.http_status === 200,
      ),
    ).toBe(true);
  } finally {
    await api(request, "command", {
      crawl_id: healthy.id,
      action: "stop",
      id: crypto.randomUUID(),
    });
  }
});
