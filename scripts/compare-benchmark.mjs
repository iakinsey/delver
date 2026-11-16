import { readFile, mkdir, writeFile } from "node:fs/promises";
import { spawnSync } from "node:child_process";
import path from "node:path";
const baselinePath = process.env.DELVER_BENCHMARK_BASELINE;
if (!baselinePath) throw Error("DELVER_BENCHMARK_BASELINE must identify a baseline captured on this host before refactoring");
const root = process.env.DELVER_VALIDATION_ROOT;
if (!root) throw Error("DELVER_VALIDATION_ROOT required");
await mkdir(root, { recursive: true });
const result = spawnSync("go", ["test", "./util", "-run", "^$", "-bench", "BenchmarkFetchCapacity", "-benchtime=1x", "-count=5"], { encoding: "utf8" });
await writeFile(path.join(root, "benchmark-final.log"), result.stdout + result.stderr);
if (result.status !== 0) throw Error("Final benchmark failed");
function samples(log) { const groups = new Map(); for (const line of log.split("\n")) { const m = line.match(/BenchmarkFetchCapacity\/(\d+)-\d+.*?([\d.]+) useful_pages\/s/); if (m) { const n = Number(m[1]); if (!groups.has(n)) groups.set(n, []); groups.get(n).push(Number(m[2])); } } return groups; }
const before = samples(await readFile(baselinePath, "utf8")), after = samples(result.stdout);
const median = values => [...values].sort((a,b) => a-b)[Math.floor(values.length/2)];
const rows = [];
for (const n of [4, 16, 32, 64]) { const a = before.get(n), b = after.get(n); if (a?.length !== 5 || b?.length !== 5) throw Error(`Missing five repetitions at ${n} slots`); const ratio = median(b) / median(a); rows.push({slots:n, baseline:a, final:b, baselineMedian:median(a), finalMedian:median(b), ratio}); }
await writeFile(path.join(root,"benchmark-comparison.json"), JSON.stringify(rows,null,2));
console.log(JSON.stringify(rows));
if (rows.some(r => r.ratio < 0.9)) throw Error("Fixed throughput regressed by more than 10%; investigate before accepting");
const pipelineBaseline=process.env.DELVER_PIPELINE_BASELINE;
if(!pipelineBaseline)throw Error("DELVER_PIPELINE_BASELINE must identify the same full-pipeline workload on the checkpoint");
const pipeline=spawnSync("go",["test","./application","-run","^$","-bench","BenchmarkWorkspaceCapacity","-benchtime=1x","-count=5"],{encoding:"utf8"});
await writeFile(path.join(root,"benchmark-pipeline-final.log"),pipeline.stdout+pipeline.stderr);
if(pipeline.status!==0)throw Error("Full pipeline benchmark failed");
function pipelineSamples(log){return [...log.matchAll(/([\d.]+) useful_pages\/s$/gm)].map(m=>Number(m[1]));}
const pipelineBefore=pipelineSamples(await readFile(pipelineBaseline,"utf8")),pipelineAfter=pipelineSamples(pipeline.stdout);
if(pipelineBefore.length!==5||pipelineAfter.length!==5)throw Error("Full pipeline comparison requires five repetitions");
const pipelineComparison={baseline:pipelineBefore,final:pipelineAfter,baselineMedian:median(pipelineBefore),finalMedian:median(pipelineAfter),ratio:median(pipelineAfter)/median(pipelineBefore)};
await writeFile(path.join(root,"benchmark-pipeline-comparison.json"),JSON.stringify(pipelineComparison,null,2));
console.log(JSON.stringify(pipelineComparison));
if(pipelineComparison.ratio<0.9)throw Error("Full pipeline fixed successful throughput regressed by more than 10%");
