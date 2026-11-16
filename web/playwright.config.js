import { defineConfig } from "@playwright/test";
import os from "node:os";
import path from "node:path";
const artifacts = (process.env.DELVER_UI_ARTIFACT_ROOT ||= path.join(
  os.tmpdir(),
  `delver-ui-artifacts-${Date.now()}-${process.pid}`,
));
export default defineConfig({
  testDir: "./e2e",
  outputDir: artifacts,
  globalSetup: "./e2e/globalSetup.mjs",
  workers: Math.min(4, os.availableParallelism()),
  retries: 0,
  globalTimeout: 25 * 60 * 1000,
  timeout: 60000,
  expect: { timeout: 15000 },
  use: {
    browserName: "chromium",
    headless: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  reporter: [
    ["line"],
    [
      "json",
      {
        outputFile:
          process.env.DELVER_BROWSER_REPORT || "test-results/results.json",
      },
    ],
  ],
});
