"""Pure-Python micro-benchmarks comparing two interpreters.

Run as:   <python> microbench.py
The script writes a single JSON object to stdout describing the result of
every registered benchmark.  Per-op timings are reported in nanoseconds and
each benchmark records its own `inner` count so the runner can normalise.

The choice of benchmarks is deliberately interpreter-bound (loops,
function dispatch, attribute access, dict / list churn, bytecode-heavy
stdlib helpers).  That is where -O3 + LTO and static linking are
expected to move the needle, in contrast to C-extension-bound work
(NumPy-style arithmetic, hashlib) where both interpreters call into the
exact same compiled implementation.
"""

import json
import re
import sys
import time
from collections import OrderedDict

BENCHMARKS = []


def benchmark(inner):
    def deco(fn):
        BENCHMARKS.append((fn.__name__, fn, inner))
        return fn

    return deco


@benchmark(inner=21_891)
def fib_recursive():
    # fib(20) does 21_891 recursive calls; normalising by that gives
    # cleanly comparable ns-per-call numbers across interpreters.
    def fib(n):
        return n if n < 2 else fib(n - 1) + fib(n - 2)

    fib(20)


@benchmark(inner=100_000)
def fib_iter():
    # Pure loop / tuple-pack / unpack benchmark.  Capped with a modulus so the
    # values stay machine-word sized and we measure interpreter overhead,
    # not Python's bignum arithmetic (which is O(N^2) over 1M iters).
    MOD = 0xFFFFFFFF
    a, b = 0, 1
    for _ in range(100_000):
        a, b = b, (a + b) & MOD


@benchmark(inner=200_000)
def arith_loop():
    s = 0
    for i in range(200_000):
        s += i * 2 - 1
    return s


@benchmark(inner=100_000)
def listcomp():
    return [x * x for x in range(100_000)]


@benchmark(inner=100_000)
def dictops():
    d = {}
    for i in range(100_000):
        d[i] = i ^ 0x5A5A
    s = 0
    for i in range(100_000):
        s += d[i]
    return s


@benchmark(inner=10_000)
def attr_access():
    class O:
        pass

    o = O()
    o.a = o.b = o.c = 1
    s = 0
    for _ in range(10_000):
        s += o.a + o.b + o.c
    return s


@benchmark(inner=50_000)
def str_format():
    s = ""
    for i in range(50_000):
        s = "{}={};".format("k", i)
    return s


@benchmark(inner=10_000)
def regex_match():
    p = re.compile(r"^(\w+)\s+(\d+)\s+(\d+\.\d+)$")
    line = "answer 42 3.14"
    n = 0
    for _ in range(10_000):
        m = p.match(line)
        if m:
            n += len(m.group(1))
    return n


@benchmark(inner=2_000)
def json_roundtrip():
    obj = {
        "ints": list(range(50)),
        "strs": ["alpha", "beta", "gamma"] * 10,
        "nested": {"x": 1, "y": [2, 3, {"z": 4}]},
    }
    s = ""
    for _ in range(2_000):
        s = json.dumps(obj)
    return json.loads(s)


@benchmark(inner=50_000)
def func_call():
    def f(a, b, c):
        return a + b + c

    s = 0
    for i in range(50_000):
        s += f(i, i + 1, i + 2)
    return s


@benchmark(inner=2_000)
def except_path():
    n = 0
    for i in range(2_000):
        try:
            if i & 1:
                raise ValueError(i)
        except ValueError:
            n += 1
    return n


# -- harness ----------------------------------------------------------------


def measure(fn, inner, runs=7, warmups=2):
    for _ in range(warmups):
        fn()
    best = float("inf")
    for _ in range(runs):
        t0 = time.perf_counter_ns()
        fn()
        dt = time.perf_counter_ns() - t0
        if dt < best:
            best = dt
    return best / inner


def main():
    results = OrderedDict()
    for name, fn, inner in BENCHMARKS:
        ns_per_op = measure(fn, inner)
        results[name] = {"ns_per_op": ns_per_op, "inner": inner}
    print(
        json.dumps(
            {
                "version": sys.version.split()[0],
                "implementation": sys.implementation.name,
                "executable": sys.executable,
                "results": results,
            }
        )
    )


if __name__ == "__main__":
    main()
