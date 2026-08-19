#!/usr/bin/env python3
"""Bounded black-box API health load probe for deployment qualification."""
import argparse
import concurrent.futures
import json
import math
import time
import urllib.request


def percentile(values, q):
    values = sorted(values)
    if not values:
        return 0.0
    rank = max(0, min(len(values) - 1, math.ceil(q * len(values)) - 1))
    return values[rank]


def request_once(url, timeout):
    started = time.perf_counter()
    ok = False
    try:
        req = urllib.request.Request(url, method="GET", headers={"User-Agent": "torgnexa-p2-qualification/1"})
        with urllib.request.urlopen(req, timeout=timeout) as response:
            body = response.read(4096)
            ok = response.status == 200 and b'"status"' in body
    except Exception:
        ok = False
    return (time.perf_counter() - started) * 1000.0, ok


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default="http://127.0.0.1:8080/api/v1/health")
    parser.add_argument("--requests", type=int, default=5000)
    parser.add_argument("--concurrency", type=int, default=64)
    parser.add_argument("--timeout", type=float, default=3.0)
    parser.add_argument("--availability-min", type=float, default=0.999)
    parser.add_argument("--p99-max-ms", type=float, default=750.0)
    parser.add_argument("--throughput-min", type=float, default=250.0)
    parser.add_argument("--output")
    args = parser.parse_args()
    if args.requests < 100 or args.requests > 1_000_000 or args.concurrency < 1 or args.concurrency > 4096:
        raise SystemExit("invalid bounded load profile")

    started = time.perf_counter()
    latencies = []
    successes = 0
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrency) as pool:
        futures = [pool.submit(request_once, args.url, args.timeout) for _ in range(args.requests)]
        for future in concurrent.futures.as_completed(futures):
            latency, ok = future.result()
            latencies.append(latency)
            successes += int(ok)
    elapsed = max(time.perf_counter() - started, 1e-9)
    availability = successes / args.requests
    throughput = args.requests / elapsed
    result = {
        "profile": "api_health_blackbox",
        "requests": args.requests,
        "concurrency": args.concurrency,
        "successes": successes,
        "availability": availability,
        "p50_ms": percentile(latencies, 0.50),
        "p95_ms": percentile(latencies, 0.95),
        "p99_ms": percentile(latencies, 0.99),
        "throughput_ops_per_second": throughput,
        "elapsed_seconds": elapsed,
        "thresholds": {
            "availability_min": args.availability_min,
            "p99_max_ms": args.p99_max_ms,
            "throughput_min": args.throughput_min,
        },
    }
    result["passed"] = (
        availability >= args.availability_min
        and result["p99_ms"] <= args.p99_max_ms
        and throughput >= args.throughput_min
    )
    encoded = json.dumps(result, indent=2, sort_keys=True) + "\n"
    if args.output:
        with open(args.output, "w", encoding="utf-8") as handle:
            handle.write(encoded)
    print(encoded, end="")
    raise SystemExit(0 if result["passed"] else 1)


if __name__ == "__main__":
    main()
