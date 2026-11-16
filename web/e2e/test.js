import { test as base, expect } from "@playwright/test";
export const test = base.extend({
  baseURL: async ({}, use) => use(process.env.DELVER_E2E_BASE_URL),
});
export { expect };
