import { readFile } from "node:fs/promises";
import { configurations, scenarioId } from "../web/e2e/configurationManifest.mjs";
const inventory = JSON.parse(await readFile(new URL("../tests/ui-coverage.json", import.meta.url), "utf8"));
const key = c => `${c.file}:${c.title}`;
const expected = new Set(inventory.cases.map(key));
if (expected.size !== inventory.cases.length) throw Error("Duplicate required UI cases");
const interactions = new Set([...configurations()].map(v => `interaction ${scenarioId(v)}`));
for (const title of interactions) if (!inventory.cases.some(c => c.title === title)) throw Error(`Interaction absent from inventory: ${title}`);
const files = process.argv.slice(2);
if (!files.length) throw Error("Usage: node scripts/check-ui-coverage.mjs REPORT.json [SHARD-REPORT.json ...]");
const seen = new Map(), failures = [];
function visit(suites) {
 for (const suite of suites || []) {
  for (const spec of suite.specs || []) {
   const id = key(spec);
   if (!expected.has(id)) failures.push(`Unregistered case: ${id}`);
   if (seen.has(id)) failures.push(`Duplicate case: ${id}`);
   const passed = spec.tests?.length === 1 && spec.tests[0].expectedStatus === "passed" && spec.tests[0].results?.length === 1 && spec.tests[0].results[0].status === "passed";
   seen.set(id, passed);
   if (!passed) failures.push(`Not passed: ${id}`);
  }
  visit(suite.suites);
 }
}
for (const file of files) {
 const report = JSON.parse(await readFile(file, "utf8"));
 if (report.errors?.length) failures.push(`${file}: runner errors`);
 visit(report.suites);
}
const missing = [...expected].filter(id => !seen.has(id));
console.log(JSON.stringify({ expected: expected.size, interactions: interactions.size, executed: seen.size, passed: [...seen.values()].filter(Boolean).length, missing: missing.length, failures: failures.length }));
if (missing.length || failures.length) throw Error([...failures, ...missing.map(id => `Missing: ${id}`)].slice(0, 25).join("\n"));
