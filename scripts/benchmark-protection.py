#!/usr/bin/env python3
"""Compare two built binaries on the same local fixed-concurrency workload."""
import argparse
import hashlib
import http.server
import json
import pathlib
import signal
import socket
import statistics
import subprocess
import tempfile
import threading
import time
import urllib.request
import urllib.parse


def rpc(base, route, data=None):
    request = urllib.request.Request(base + route, data=json.dumps(data or {}).encode(), headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(request, timeout=30) as response:
        value = json.load(response)
    if not value.get("success"):
        raise RuntimeError(value.get("error", value))
    return value["data"]


def available_port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def checksum(path):
    digest = hashlib.sha256()
    with open(path, "rb") as source:
        for part in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(part)
    return digest.hexdigest()


def sample(binary, pages, delay, directory, definition_transform=None, source_mode="discoveries"):
    expected = pages + (source_mode == "discoveries")
    directory.mkdir()
    requests = []
    request_lock = threading.Lock()

    class Fixture(http.server.BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def do_GET(self):
            started = time.monotonic()
            time.sleep(delay)
            if source_mode == "discoveries" and urllib.parse.urlsplit(self.path).hostname == "benchmark-root.invalid":
                links = "".join(f'<a href="http://benchmark-{n}.invalid/page/{n}">Page {n}</a>' for n in range(pages))
                body = ("<title>Controlled root</title><main>" + links + "</main>").encode()
            else:
                body = f"<title>Page {self.path}</title><p>Unique content {self.path}</p>".encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            with request_lock:
                requests.append({"path": self.path, "user_agent": self.headers.get("User-Agent"), "seconds": time.monotonic() - started, "finished_at": time.monotonic()})

        def log_message(self, *_):
            pass

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    port = available_port()
    base = f"http://127.0.0.1:{port}"
    with (directory / "application.log").open("wb") as log:
        process = subprocess.Popen([str(binary), "-listen", f"127.0.0.1:{port}"], cwd=directory, stdout=log, stderr=log)
        try:
            deadline = time.monotonic() + 30
            while True:
                if process.poll() is not None:
                    raise RuntimeError(f"binary exited: {directory / 'application.log'}")
                try:
                    definition = rpc(base, "/crawls/defaults")
                    break
                except (OSError, RuntimeError):
                    if time.monotonic() > deadline:
                        raise
                    time.sleep(0.05)
            definition.update({"name": "Controlled fixed capacity", "scope": "open_web", "request_delay": "0s", "termination": {"when_drained": True, "max_requests": 0, "duration": ""}, "seeds": [f"http://benchmark-{n}.invalid/page/{n}" for n in range(pages)], "proxy": {"mode": "http", "url": f"http://127.0.0.1:{server.server_port}"}})
            if source_mode == "discoveries":
                definition["seeds"] = ["http://benchmark-root.invalid/"]
            definition["network"].update({"respect_robots": False, "require_proxy": True, "user_agent": "DelverProtectionBenchmark/1.0"})
            definition["fetch"].update({"mode": "fixed", "concurrency": 32, "maximum": 32})
            if definition_transform is not None:
                definition_transform(definition)
            record = rpc(base, "/crawls/create", {"definition": definition})
            started = time.monotonic()
            rpc(base, "/crawls/start_run", {"crawl_id": record["id"], "revision": record["revision"], "id": "benchmark-start"})
            while time.monotonic() - started < 300:
                detail = rpc(base, "/crawls/detail", {"crawl_id": record["id"]})
                if not detail.get("runs"):
                    if detail.get("runtime", {}).get("state") == "failed":
                        raise RuntimeError(f"crawl initialization failed: {detail['runtime']}")
                    time.sleep(0.25)
                    continue
                run = detail["runs"][-1]
                if run["state"] == "completed":
                    break
                if run["state"] == "failed":
                    raise RuntimeError(f"crawl failed: {detail['runtime']}")
                time.sleep(0.25)
            else:
                raise RuntimeError("controlled crawl did not complete")
            elapsed = time.monotonic() - started
            frame = run["summary"]
            totals = frame["fetch"]["totals"]
            if totals["attempts"] != expected or totals["successes"] != expected or len(requests) != expected:
                raise RuntimeError(f"unexpected completed workload: {totals}, destination requests={len(requests)}")
            if any(row["user_agent"] != "DelverProtectionBenchmark/1.0" for row in requests):
                raise RuntimeError("User-Agent differs from controlled configuration")
            if frame["fetch"]["active"] != 0 or frame["fetch"]["network_active"] != 0:
                raise RuntimeError("completed crawl retains active requests")
            proc_status = pathlib.Path(f"/proc/{process.pid}/status").read_text()
            memory = {line.split(":", 1)[0]: line.split(":", 1)[1].strip() for line in proc_status.splitlines() if line.startswith(("VmRSS:", "VmHWM:", "Threads:"))}
            return {"elapsed_seconds": elapsed, "successes_per_second": expected / elapsed, "definition": definition, "frame": frame, "process": memory, "network": requests}
        finally:
            process.send_signal(signal.SIGTERM)
            try:
                process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
                raise RuntimeError("binary did not shut down")
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", type=pathlib.Path, required=True)
    parser.add_argument("--final", type=pathlib.Path, required=True)
    parser.add_argument("--output", type=pathlib.Path, required=True)
    parser.add_argument("--pages", type=int, default=4096)
    parser.add_argument("--delay-ms", type=float, default=20)
    parser.add_argument("--storage-root", type=pathlib.Path, required=True)
    parser.add_argument("--source", choices=["discoveries", "seeds"], default="discoveries")
    args = parser.parse_args()
    if args.pages < 32 or args.delay_ms <= 0:
        parser.error("pages must be >=32 and delay must be positive")
    binaries = {"baseline": args.baseline.resolve(strict=True), "final": args.final.resolve(strict=True)}
    args.output.mkdir(parents=True, exist_ok=True)
    args.storage_root.mkdir(parents=True, exist_ok=True)
    filesystem = subprocess.check_output(["stat", "-f", "-c", "%T", str(args.storage_root)], text=True).strip()
    if filesystem != "tmpfs":
        parser.error("controlled capacity comparison requires a tmpfs storage root; persistent disk lifecycle has a separate gate")
    report = {"binary_sha256": {key: checksum(value) for key, value in binaries.items()}, "pages": args.pages, "delay_ms": args.delay_ms, "repetitions": 5, "storage_root": str(args.storage_root.resolve()), "filesystem": filesystem, "source": args.source, "status_poll_seconds": 0.25, "samples": {"baseline": [], "final": []}}
    root = tempfile.mkdtemp(prefix="delver-protection-benchmark-", dir=args.storage_root)
    for repeat in range(5):
        for key in (["baseline", "final"] if repeat % 2 == 0 else ["final", "baseline"]):
            report["samples"][key].append(sample(binaries[key], args.pages, args.delay_ms / 1000, pathlib.Path(root) / f"{repeat}-{key}", source_mode=args.source))
            (args.output / "protection-benchmark.json").write_text(json.dumps(report, indent=2))
            print(json.dumps({"repetition": repeat + 1, "binary": key, "successes_per_second": report["samples"][key][-1]["successes_per_second"]}), flush=True)
    medians = {key: statistics.median(row["successes_per_second"] for row in values) for key, values in report["samples"].items()}
    variation = {key: statistics.stdev(row["successes_per_second"] for row in values) / statistics.mean(row["successes_per_second"] for row in values) for key, values in report["samples"].items()}
    report["coefficient_of_variation"] = variation
    report["stable"] = max(variation.values()) <= 0.1
    report["median_successes_per_second"] = medians
    report["final_to_baseline"] = medians["final"] / medians["baseline"]
    report["passed"] = report["stable"] and report["final_to_baseline"] >= 0.9
    if report["binary_sha256"]["baseline"] == report["binary_sha256"]["final"]:
        report["passed"] = report["passed"] and abs(report["final_to_baseline"] - 1) <= 0.05
    (args.output / "protection-benchmark.json").write_text(json.dumps(report, indent=2))
    print(json.dumps({"medians": medians, "ratio": report["final_to_baseline"], "passed": report["passed"]}))
    if not report["passed"]:
        raise SystemExit("comparison failed: instability, identical-binary drift, or throughput regression above 10%; inspect raw measurements")


if __name__ == "__main__":
    main()
