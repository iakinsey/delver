import { readFileSync, mkdirSync } from "node:fs";
import { spawnSync } from "node:child_process";
import path from "node:path";
const inventory=JSON.parse(readFileSync(new URL("../tests/ui-coverage.json",import.meta.url),"utf8"));
console.log(`Expected browser cases: ${inventory.cases.length}; invocation limit: 30 minutes`);
const root=process.env.DELVER_VALIDATION_ROOT;
if(!root)throw Error("DELVER_VALIDATION_ROOT required");
mkdirSync(root,{recursive:true});
const report=path.resolve(root,"browser-results.json"),started=Date.now();
for(const [cmd,args] of [["npm",["--prefix","web","run","build"]],["node",["scripts/package-ui.mjs"]],["npm",["--prefix","web","run","test:browser"]],["node",["scripts/check-ui-coverage.mjs",report]]]){
 const remaining=29*60*1000-(Date.now()-started);
 if(remaining<=0)throw Error("UI invocation deadline exceeded");
 const result=spawnSync(cmd,args,{stdio:"inherit",timeout:remaining,env:{...process.env,DELVER_BROWSER_REPORT:report}});
 if(result.error)throw result.error;
 if(result.status!==0)process.exit(result.status||1);
}
console.log(`UI invocation completed in ${((Date.now()-started)/1000).toFixed(1)}s`);
