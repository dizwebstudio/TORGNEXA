#!/usr/bin/env python3
"""Bounded black-box health or authenticated API/SSE qualification load."""

import argparse
import concurrent.futures
import json
import math
import os
import stat
import threading
import time
import urllib.parse
import urllib.request


def percentile(values, quantile):
    values = sorted(values)
    if not values:
        return 0.0
    rank = max(0, min(len(values) - 1, math.ceil(quantile * len(values)) - 1))
    return values[rank]


def request_once(url, timeout, token="", expect_health=False):
    started = time.perf_counter()
    ok = False
    try:
        headers = {"User-Agent": "torgnexa-qualification/1", "Accept": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        request = urllib.request.Request(url, method="GET", headers=headers)
        with urllib.request.urlopen(request, timeout=timeout) as response:
            body = response.read(1_048_577)
            content_type = response.headers.get("Content-Type", "").lower()
            ok = 200 <= response.status < 300 and len(body) <= 1_048_576
            if expect_health:
                ok = ok and b'"status"' in body
            else:
                ok = ok and "json" in content_type
    except Exception:
        ok = False
    return (time.perf_counter() - started) * 1000.0, ok


def load_token(path):
    token_path = os.path.abspath(path)
    metadata = os.lstat(token_path)
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode) or stat.S_IMODE(metadata.st_mode) & 0o077:
        raise ValueError("token file must be a private regular file")
    with open(token_path, "r", encoding="ascii") as handle:
        token = handle.read(32769)
    if len(token) < 32 or len(token) > 32768 or token.strip() != token or token.count(".") != 2:
        raise ValueError("token file does not contain one bounded JWT")
    return token


def bounded_base_url(value):
    parsed = urllib.parse.urlsplit(value)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError("base URL must be an HTTP(S) origin")
    path = parsed.path.rstrip("/")
    return urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, path, "", ""))


def bounded_path(value):
    parsed = urllib.parse.urlsplit(value)
    if not value.startswith("/") or parsed.scheme or parsed.netloc or parsed.fragment:
        raise ValueError("load paths must be origin-relative")
    return urllib.parse.urlunsplit(("", "", parsed.path, parsed.query, ""))


class SSECoordinator:
    def __init__(self, expected):
        self.expected = expected
        self.connected = 0
        self.peak = 0
        self.lock = threading.Lock()
        self.all_ready = threading.Event()
        self.release = threading.Event()

    def enter(self):
        with self.lock:
            self.connected += 1
            self.peak = max(self.peak, self.connected)
            if self.connected == self.expected:
                self.all_ready.set()

    def leave(self):
        with self.lock:
            self.connected -= 1


def sse_once(url, token, timeout, coordinator):
    started = time.perf_counter()
    connect_latency = None
    entered = False
    ok = False
    try:
        request = urllib.request.Request(
            url,
            method="GET",
            headers={
                "Authorization": "Bearer " + token,
                "Accept": "text/event-stream",
                "Cache-Control": "no-cache",
                "User-Agent": "torgnexa-qualification-sse/1",
            },
        )
        with urllib.request.urlopen(request, timeout=timeout) as response:
            content_type = response.headers.get("Content-Type", "").lower()
            frame = []
            total = 0
            while total <= 16_384:
                line = response.readline(4096)
                total += len(line)
                if not line:
                    break
                if line in {b"\n", b"\r\n"}:
                    break
                frame.append(line)
            ok = response.status == 200 and "text/event-stream" in content_type and any(line.startswith(b"event: ready") for line in frame)
            if ok:
                connect_latency = (time.perf_counter() - started) * 1000.0
                coordinator.enter()
                entered = True
                coordinator.release.wait(timeout)
    except Exception:
        ok = False
    finally:
        if entered:
            coordinator.leave()
    if connect_latency is None:
        connect_latency = (time.perf_counter() - started) * 1000.0
    return connect_latency, ok


def distribution(latencies):
    return {
        "p50_ms": percentile(latencies, 0.50),
        "p95_ms": percentile(latencies, 0.95),
        "p99_ms": percentile(latencies, 0.99),
    }


def write_report(path, encoded):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            handle.write(encoded)
        if stat.S_IMODE(os.stat(path).st_mode) != 0o600:
            raise ValueError("report file permissions are not private")
    except Exception:
        try:
            os.unlink(path)
        except FileNotFoundError:
            pass
        raise


def run_health(args):
    started = time.perf_counter()
    latencies = []
    successes = 0
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrency) as pool:
        futures = [pool.submit(request_once, args.url, args.timeout, "", True) for _ in range(args.requests)]
        for future in concurrent.futures.as_completed(futures):
            latency, ok = future.result()
            latencies.append(latency)
            successes += int(ok)
    elapsed = max(time.perf_counter() - started, 1e-9)
    result = {
        "profile": "api_health_blackbox",
        "requests": args.requests,
        "concurrency": args.concurrency,
        "successes": successes,
        "availability": successes / args.requests,
        **distribution(latencies),
        "throughput_ops_per_second": args.requests / elapsed,
        "elapsed_seconds": elapsed,
        "thresholds": {"availability_min": args.availability_min, "p99_max_ms": args.p99_max_ms, "throughput_min": args.throughput_min},
    }
    result["passed"] = result["availability"] >= args.availability_min and result["p99_ms"] <= args.p99_max_ms and result["throughput_ops_per_second"] >= args.throughput_min
    return result


def run_authenticated(args):
    base_url = bounded_base_url(args.base_url)
    paths = [bounded_path(path) for path in args.path]
    if len(set(paths)) < 2:
        raise ValueError("authenticated mix requires at least two distinct paths")
    token = load_token(args.token_file)
    warmup_latency, warmup_ok = request_once(base_url + paths[0], args.timeout, token)
    if not warmup_ok:
        raise ValueError("authenticated warmup request failed")

    coordinator = SSECoordinator(args.sse_clients)
    sse_url = base_url + bounded_path(args.sse_path)
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.sse_clients) as sse_pool:
        sse_futures = [sse_pool.submit(sse_once, sse_url, token, args.sse_timeout, coordinator) for _ in range(args.sse_clients)]
        coordinator.all_ready.wait(args.sse_ready_timeout)
        started = time.perf_counter()
        latencies = []
        successes = 0
        route_data = {path: {"requests": 0, "successes": 0, "latencies": []} for path in paths}
        try:
            with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrency) as pool:
                requests = []
                for index in range(args.requests):
                    path = paths[index % len(paths)]
                    requests.append((path, pool.submit(request_once, base_url + path, args.timeout, token)))
                for path, future in requests:
                    latency, ok = future.result()
                    latencies.append(latency)
                    successes += int(ok)
                    route_data[path]["requests"] += 1
                    route_data[path]["successes"] += int(ok)
                    route_data[path]["latencies"].append(latency)
        finally:
            coordinator.release.set()
        elapsed = max(time.perf_counter() - started, 1e-9)
        sse_results = [future.result() for future in sse_futures]

    sse_latencies = [latency for latency, _ in sse_results]
    sse_successes = sum(int(ok) for _, ok in sse_results)
    routes = []
    for path, item in route_data.items():
        routes.append({"path": path, "requests": item["requests"], "successes": item["successes"], "availability": item["successes"] / item["requests"], **distribution(item["latencies"])})
    http_result = {
        "requests": args.requests,
        "concurrency": args.concurrency,
        "successes": successes,
        "availability": successes / args.requests,
        **distribution(latencies),
        "throughput_ops_per_second": args.requests / elapsed,
        "elapsed_seconds": elapsed,
        "routes": routes,
    }
    sse_result = {"clients": args.sse_clients, "successes": sse_successes, "availability": sse_successes / args.sse_clients, "peak_connected": coordinator.peak, **distribution(sse_latencies)}
    result = {
        "profile": "authenticated_api_and_sse",
        "warmup": {"passed": True, "latency_ms": warmup_latency},
        "http": http_result,
        "sse": sse_result,
        "thresholds": {
            "availability_min": args.availability_min,
            "p99_max_ms": args.p99_max_ms,
            "throughput_min": args.throughput_min,
            "sse_availability_min": args.sse_availability_min,
            "sse_p99_max_ms": args.sse_p99_max_ms,
        },
        "redacted": True,
    }
    result["passed"] = (
        http_result["availability"] >= args.availability_min
        and http_result["p99_ms"] <= args.p99_max_ms
        and http_result["throughput_ops_per_second"] >= args.throughput_min
        and sse_result["availability"] >= args.sse_availability_min
        and sse_result["p99_ms"] <= args.sse_p99_max_ms
        and sse_result["peak_connected"] == args.sse_clients
    )
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--profile", choices=("health", "authenticated"), default="health")
    parser.add_argument("--url", default="http://127.0.0.1:8080/api/v1/health")
    parser.add_argument("--base-url", default="")
    parser.add_argument("--path", action="append", default=[])
    parser.add_argument("--token-file", default="")
    parser.add_argument("--sse-path", default="/api/v1/realtime")
    parser.add_argument("--sse-clients", type=int, default=32)
    parser.add_argument("--sse-timeout", type=float, default=20.0)
    parser.add_argument("--sse-ready-timeout", type=float, default=12.0)
    parser.add_argument("--requests", type=int, default=5000)
    parser.add_argument("--concurrency", type=int, default=64)
    parser.add_argument("--timeout", type=float, default=3.0)
    parser.add_argument("--availability-min", type=float, default=0.999)
    parser.add_argument("--p99-max-ms", type=float, default=750.0)
    parser.add_argument("--throughput-min", type=float, default=250.0)
    parser.add_argument("--sse-availability-min", type=float, default=1.0)
    parser.add_argument("--sse-p99-max-ms", type=float, default=20_000.0)
    parser.add_argument("--output")
    args = parser.parse_args()
    if args.requests < 100 or args.requests > 1_000_000 or args.concurrency < 1 or args.concurrency > 4096:
        raise SystemExit("invalid bounded load profile")
    if args.sse_clients < 1 or args.sse_clients > 64:
        raise SystemExit("invalid bounded SSE client count")
    try:
        result = run_health(args) if args.profile == "health" else run_authenticated(args)
    except (OSError, ValueError) as error:
        raise SystemExit(f"runtime-load: {error}") from error
    encoded = json.dumps(result, indent=2, sort_keys=True) + "\n"
    if args.output:
        write_report(args.output, encoded)
    print(encoded, end="")
    raise SystemExit(0 if result["passed"] else 1)


if __name__ == "__main__":
    main()
