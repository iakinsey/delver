import { spawn, execFile } from "node:child_process";
import { mkdtemp, readFile, rm, open } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

export async function startFixture({ legacy = false } = {}) {
  const root = await mkdtemp(path.join(tmpdir(), "delver-browser-"));
  const stateFile = path.join(root, "fixture.json"),
    binary = path.join(root, "fixture");
  const log = await open(path.join(root, "fixture.log"), "a");
  const repository = path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    "../..",
  );
  await promisify(execFile)(
    "go",
    ["build", "-o", binary, "./examples/workspace-fixture"],
    { cwd: repository },
  );
  let child, url, info;
  const stopChild = async () => {
    if (!child || child.exitCode !== null || child.signalCode !== null) return;
    const exited = new Promise((resolve) => child.once("exit", resolve));
    try {
      process.kill(-child.pid, "SIGTERM");
    } catch (error) {
      if (error.code !== "ESRCH") throw error;
    }
    const graceful = await Promise.race([
      exited.then(() => true),
      new Promise((resolve) => setTimeout(() => resolve(false), 15000)),
    ]);
    if (!graceful) {
      process.kill(-child.pid, "SIGKILL");
      await exited;
      throw Error("Fixture did not shut down within 15s");
    }
  };
  const launch = async (address = "127.0.0.1:0") => {
    await rm(stateFile, { force: true });
    const bindings = {};
    for (const [key, value] of Object.entries({
      SOURCE: info?.source,
      PROXY: info?.proxy,
      SOCKS: info?.socks,
      TLS_SOURCE: info?.tls_source,
      TLS_PROXY: info?.https_proxy,
    }))
      if (value)
        bindings[`DELVER_WORKSPACE_FIXTURE_${key}_ADDR`] = new URL(value).host;
    child = spawn(binary, [], {
      cwd: repository,
      detached: true,
      env: {
        ...process.env,
        DELVER_WORKSPACE_FIXTURE_LEGACY: legacy ? "1" : "0",
        ...bindings,
        DELVER_WORKSPACE_FIXTURE_ADDR: address,
        DELVER_WORKSPACE_FIXTURE_ROOT: path.join(root, "storage"),
        DELVER_WORKSPACE_FIXTURE_STATE_FILE: stateFile,
      },
      stdio: ["ignore", log.fd, log.fd],
    });
    const deadline = Date.now() + 60000;
    while (Date.now() < deadline) {
      if (child.exitCode !== null)
        throw Error(await readFile(path.join(root, "fixture.log"), "utf8"));
      try {
        const state = JSON.parse(await readFile(stateFile, "utf8"));
        const response = await fetch(state.url + "/fixture/info");
        if (response.ok) {
          url = state.url;
          info = await response.json();
          return;
        }
      } catch {}
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    throw Error("Browser fixture startup timeout: " + root);
  };
  try {
    await launch();
  } catch (error) {
    await stopChild();
    await log.close();
    throw error;
  }
  return {
    storage: path.join(root, "storage"),
    get url() {
      return url;
    },
    restart: async () => {
      const address = new URL(url).host;
      await stopChild();
      await launch(address);
    },
    stop: async () => {
      await stopChild();
      await log.close();
      if (process.env.DELVER_KEEP_TEST_DATA !== "1")
        await rm(root, { recursive: true, force: true });
    },
  };
}
export default async function setup() {
  const fixture = await startFixture();
  process.env.DELVER_E2E_BASE_URL = fixture.url;
  return fixture.stop;
}
