"""Measure cold-start / first-import latency for a Python interpreter.

The harness is host-side (we always run it under the system python so the
results are independent of the interpreter being measured).  Each scenario
spawns the candidate interpreter many times and reports min / median wall
time in milliseconds.

Scenarios:
  bare    -> python -c "pass"
  sysmod  -> python -c "import sys"
  stdlib  -> python -c "import json, re, os, sys, collections, hashlib"

Usage:  python measure_startup.py <interpreter> [iters]
"""

import json
import statistics
import subprocess
import sys
import time

SCENARIOS = {
    "bare": ["-c", "pass"],
    "sysmod": ["-c", "import sys"],
    "stdlib": ["-c", "import json, re, os, sys, collections, hashlib"],
}


def time_one(argv):
    t0 = time.perf_counter_ns()
    subprocess.run(argv, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    return (time.perf_counter_ns() - t0) / 1_000_000.0


def measure(interp, iters):
    out = {}
    for name, args in SCENARIOS.items():
        argv = [interp, *args]
        for _ in range(3):
            time_one(argv)
        samples = [time_one(argv) for _ in range(iters)]
        out[name] = {
            "min_ms": min(samples),
            "median_ms": statistics.median(samples),
            "samples": iters,
        }
    return out


def main():
    interp = sys.argv[1]
    iters = int(sys.argv[2]) if len(sys.argv) > 2 else 50
    res = measure(interp, iters)
    print(json.dumps({"executable": interp, "scenarios": res}))


if __name__ == "__main__":
    main()
