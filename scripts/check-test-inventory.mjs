import { readFile } from "node:fs/promises";
const inventory = JSON.parse(await readFile(new URL("../tests/inventory.json", import.meta.url), "utf8"));
if (inventory.length !== 99) throw Error(`Expected 99 original Go test files; got ${inventory.length}`);
for (const entry of inventory) {
 if (!entry.status || !entry.coverage || !entry.current_tests?.length) throw Error(`Unaccounted test: ${entry.original}`);
 const source=await readFile(new URL("../"+entry.coverage,import.meta.url),"utf8");
 const actual=new Set([...source.matchAll(/^func ((?:Test|Benchmark|Fuzz|Example)\w*)\(/gm)].map(m=>m[1]));
 for(const name of entry.current_tests)if(!actual.has(name))throw Error(`Missing mapped assertion ${name}: ${entry.original}`);
}
console.log(`Original Go test inventory: ${inventory.length} files accounted for with runnable assertions`);
