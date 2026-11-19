import { readFile, mkdir, writeFile } from "node:fs/promises";
import { spawnSync } from "node:child_process";
import path from "node:path";

const root = process.env.DELVER_VALIDATION_ROOT;
const diskBaseline = process.env.DELVER_DISCOVERY_BASELINE;
const pipelineBaseline = process.env.DELVER_DISCOVERY_PIPELINE_BASELINE;
if (!root || !diskBaseline || !pipelineBaseline) {
  throw Error("VALIDATION_ROOT, DISCOVERY_BASELINE and DISCOVERY_PIPELINE_BASELINE must identify private evidence paths");
}
const temporary = path.join(root, "discovery-disk-tmp");
await mkdir(temporary, { recursive: true });
function samples(log, benchmark) {
  const groups = new Map();
  let current;
  for (const line of log.split("\n")) {
    const match = line.match(new RegExp(`^${benchmark}/([a-z-]+)-\\d+`));
    if (match) current = match[1];
    if (!current || !line.includes(" ns/op")) continue;
    const metrics = Object.fromEntries([...line.matchAll(/([\d.]+)\s+([a-z_]+\/[a-z]+|analyzed_peak)\b/g)].map(m => [m[2], Number(m[1])]));
    if (!groups.has(current)) groups.set(current, []);
    groups.get(current).push(metrics);
  }
  return groups;
}
const median = values => [...values].sort((a, b) => a - b)[Math.floor(values.length / 2)];
const results = [];
for (const workload of [
  { name: "disk", pkg: "./frontier", benchmark: "BenchmarkDiscoveryExtensionBatch", baseline: diskBaseline, cases: ["excluded", "mixed", "allowed"], metric: "discovery_ms/batch" },
  { name: "pipeline", pkg: "./application", benchmark: "BenchmarkWorkspaceDiscoveryPressure", baseline: pipelineBaseline, cases: ["excluded-heavy", "mixed", "allowed"], metric: "pipeline_ms/run" },
]) {
  const execution = spawnSync("go", ["test", workload.pkg, "-run", "^$", "-bench", `^${workload.benchmark}$`, "-benchtime=1x", "-count=5"], {
    encoding: "utf8", maxBuffer: 64 * 1024 * 1024,
    env: { ...process.env, ...(workload.name === "disk" ? { TMPDIR: temporary } : {}) },
  });
  await writeFile(path.join(root, `discovery-${workload.name}-final.log`), execution.stdout + execution.stderr);
  if (execution.status !== 0) throw Error(`${workload.name} discovery benchmark failed`);
  const before = samples(await readFile(workload.baseline, "utf8"), workload.benchmark);
  const after = samples(execution.stdout, workload.benchmark);
  for (const name of workload.cases) {
    const a = before.get(name), b = after.get(name);
    if (a?.length !== 5 || b?.length !== 5) throw Error(`Expected five measurements for ${workload.name}/${name}`);
    const beforeMedian = median(a.map(x => x[workload.metric]));
    const afterMedian = median(b.map(x => x[workload.metric]));
    results.push({ workload: workload.name, case: name, metric: workload.metric, baseline: a, final: b, beforeMedian, afterMedian, ratio: afterMedian / beforeMedian });
  }
}
await writeFile(path.join(root, "discovery-comparison.json"), JSON.stringify(results, null, 2));
console.log(JSON.stringify(results.map(({ baseline, final, ...row }) => row)));
for (const row of results) {
  if (row.case === "allowed" && row.ratio > 1.1) throw Error(`${row.workload} allowed-only workload regressed by more than 10%`);
  if (row.case === "excluded" && row.final.some(x => x["commits/batch"] !== 16)) throw Error("Excluded discoveries must commit once per 128-input chunk");
  if (row.case === "excluded-heavy" && row.final.some(x => x["policy_skips/run"] !== 65536)) throw Error("Pipeline excluded-link counts must be exact");
}
