import { readFileSync, writeFileSync, mkdirSync, rmSync } from "node:fs";
import { spawnSync } from "node:child_process";
import path from "node:path";
const inventory=JSON.parse(readFileSync(new URL("../tests/ui-coverage.json",import.meta.url),"utf8"));
console.log(`Expected browser cases: ${inventory.cases.length}; invocation limit: 30 minutes`);
const root=process.env.DELVER_VALIDATION_ROOT;
if(!root)throw Error("DELVER_VALIDATION_ROOT required");
mkdirSync(root,{recursive:true});
const report=path.resolve(root,"browser-results.json"),started=Date.now();
rmSync(report,{force:true});
const isolated=inventory.cases.filter(c=>c.isolation==="process");
if(inventory.cases.some(c=>c.isolation&&c.isolation!=="process"))throw Error("Unknown browser isolation mode");
const escape=value=>value.replace(/[.*+?^${}()|[\]\\]/g,"\\$&");
const run=(cmd,args,output=report)=>{
 const remaining=29*60*1000-(Date.now()-started);
 if(remaining<=0)throw Error("UI invocation deadline exceeded");
 const result=spawnSync(cmd,args,{stdio:"inherit",timeout:remaining,env:{...process.env,DELVER_BROWSER_REPORT:output}});
 if(result.error)throw result.error;
 if(result.status!==0)process.exit(result.status||1);
};
run("npm",["--prefix","web","run","build"]);
run("node",["scripts/package-ui.mjs"]);
// Each invocation owns a fresh backend. Scale-fixture seeding and graph
// generation must not compete with ordinary workspace mutations.
const phases=[{name:"ordinary",args:isolated.length?["--grep-invert",`(${isolated.map(c=>escape(c.title)).join("|")})$`]:[]},...isolated.map((c,i)=>({name:`isolated-${i+1}`,args:[c.file,"--grep",`${escape(c.title)}$`]}))];
const reports=[];
for(const phase of phases){
 const output=path.resolve(root,`browser-${phase.name}-results.json`);
 rmSync(output,{force:true});
 console.log(`Browser phase: ${phase.name}`);
 run("npm",["--prefix","web","run","test:browser","--",...phase.args],output);
 reports.push({file:output,data:JSON.parse(readFileSync(output,"utf8"))});
}
// Retain every result unchanged; the inventory gate rejects missing,
// duplicated, skipped and failed cases across the complete aggregate.
const combined={...reports[0].data,suites:reports.flatMap(r=>r.data.suites||[]),errors:reports.flatMap(r=>r.data.errors||[])};
combined.stats={startTime:reports[0].data.stats.startTime,duration:Date.now()-started};
for(const key of ["expected","unexpected","skipped","flaky"])combined.stats[key]=reports.reduce((n,r)=>n+r.data.stats[key],0);
combined.shard_reports=reports.map(r=>r.file);
writeFileSync(report,JSON.stringify(combined));
run("node",["scripts/check-ui-coverage.mjs",report]);
console.log(`UI invocation completed in ${((Date.now()-started)/1000).toFixed(1)}s`);
