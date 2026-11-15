import { cp, mkdir, readdir, rm } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = path.join(root, "web", "build"),
  target = path.join(root, "web", "assets");
await mkdir(target, { recursive: true });
for (const entry of await readdir(target))
  await rm(path.join(target, entry), { recursive: true, force: true });
await cp(source, target, { recursive: true });
console.log("Bundled frontend copied to web/assets for Go embedding.");
