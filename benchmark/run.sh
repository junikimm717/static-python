#!/bin/sh
# Compare an arbitrary set of Python interpreters on the host architecture.
# Always native-vs-native: the default lineup is the in-tree static build, the
# stock dynamic build (if built), and the container's system python.
#
# Arch is detected from `uname -m`; the interpreter binary name is derived
# from PYTHONV in the project Makefile, so no architecture or version is
# hard-coded here.
#
# Env knobs (each path is best-effort except STATIC which is required):
#   HOST_ARCH=...        override `uname -m`
#   STATIC=/path/...     override the static interpreter
#   DYNAMIC=/path/...    override the dynamic interpreter
#   SYSTEM=/path/...     override the system interpreter
#   ITERS=N              startup probe iterations per scenario (default 40)
#
# Outputs:
#   - a markdown report on stdout
#   - the same report saved at benchmark/reports/<UTC-stamp>_<arch>.md,
#     ending in an empty `## Analysis` section that the running agent is
#     expected to fill in. See AGENTS.md ("Benchmarking workflow") for the
#     full loop.

set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"

HOST_ARCH="${HOST_ARCH:-$(uname -m)}"
# Some Makefile $(info ...) calls fire at parse time; tail -n1 isolates the
# echoed value of $(PYTHONV).
PYTHONV="$(make -C "$ROOT" -s print-PYTHONV 2>/dev/null | tail -n1)"
PY_BIN="python${PYTHONV}"
TARGET="${HOST_ARCH}-linux-musl"

STATIC="${STATIC:-$ROOT/python-static-${TARGET}/bin/${PY_BIN}}"
DYNAMIC="${DYNAMIC:-$ROOT/python-dynamic-${TARGET}/bin/${PY_BIN}}"
SYSTEM="${SYSTEM:-/usr/bin/python3}"
ITERS="${ITERS:-40}"

if [ ! -x "$STATIC" ]; then
    cat >&2 <<EOF
error: static interpreter not found (or not executable):
    $STATIC

build it from the dev container:
    docker compose exec spython make
EOF
    exit 1
fi

# Build a space-separated list of "label=path" tokens; non-static entries are
# included only when the path resolves to an executable.  Missing entries get
# a one-line stderr hint so the user knows the comparison will be narrower
# than expected and what to do about it.
INTERPS="static=$STATIC"
if [ -x "$DYNAMIC" ]; then
    INTERPS="$INTERPS dynamic=$DYNAMIC"
else
    echo "note: no dynamic interpreter at $DYNAMIC -- skipping" >&2
    echo "      build one with: ./benchmark/dynamic-build.sh" >&2
fi
if [ -x "$SYSTEM" ]; then
    INTERPS="$INTERPS system=$SYSTEM"
else
    echo "note: no system interpreter at $SYSTEM -- skipping" >&2
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

iec() {
    awk -v b="$1" '
        BEGIN {
            u="B";
            if (b>=1024) {b/=1024; u="K"}
            if (b>=1024) {b/=1024; u="M"}
            if (b>=1024) {b/=1024; u="G"}
            printf("%.1f%s", b, u);
        }'
}

# Returns "static" | "dynamic" | "unknown".
linkage_of() {
    file -b "$(readlink -f "$1")" 2>/dev/null \
        | grep -oE 'statically linked|dynamically linked' \
        | awk '{print $1}'
}

# For a dynamically linked python, the on-disk footprint of "running it" is
# stub + libpython.  This collapses to plain stat for a static build.
size_label_for() {
    bin="$1"
    real=$(readlink -f "$bin")
    bytes=$(stat -Lc%s "$real")
    libpython=$(ldd "$real" 2>/dev/null | awk '/libpython/ {print $3; exit}')
    if [ -n "${libpython:-}" ] && [ -e "$libpython" ]; then
        lib_bytes=$(stat -Lc%s "$libpython")
        total=$((bytes + lib_bytes))
        printf '%s stub + %s libpython = %s\n' \
            "$(iec "$bytes")" "$(iec "$lib_bytes")" "$(iec "$total")"
    else
        iec "$bytes"
    fi
}

# Pulls one cache size out of /sys; works for L1d/L1i/L2/L3 by index. Returns
# "?" when /sys is absent (e.g. a stripped-down container or a non-Linux box).
cache_size_at() {
    f="/sys/devices/system/cpu/cpu0/cache/index$1/size"
    if [ -r "$f" ]; then cat "$f"; else echo "?"; fi
}

# Best-effort CPU model name. x86 exposes it as `model name`, aarch64 / arm
# typically expose `Model name` or fall back to `Hardware`. RISC-V varies.
cpu_model() {
    awk -F': ' '
        /^model name/ {print $2; exit}
        /^Model name/ {print $2; exit}
        /^Hardware/   {print $2; exit}
    ' /proc/cpuinfo 2>/dev/null
}

# ---------------------------------------------------------------------------
# Step 1: render the header table.
# ---------------------------------------------------------------------------

# UTC stamp used both in the report header and in the saved filename. Chosen
# so the filename sorts lexically and contains no characters that need
# quoting on any reasonable filesystem.
STAMP="$(date -u '+%Y-%m-%dT%H%MZ')"

{
    echo "# benchmark report -- ${HOST_ARCH} -- ${STAMP}"
    echo
    echo "Generated: $(date -u '+%F %T UTC')"
    echo

    # --- Environment ------------------------------------------------------
    # The numbers below are meaningless without knowing what they ran on, so
    # capture the bits that actually move benchmarks (model, core count,
    # cache hierarchy, kernel). On non-x86 archs we degrade gracefully when
    # /proc/cpuinfo doesn't expose `model name`.
    container_tag=""
    [ -e /.dockerenv ] && container_tag=" (inside container)"
    model="$(cpu_model)"
    [ -n "$model" ] || model="unknown"
    cores="$(nproc 2>/dev/null || echo "?")"
    kernel="$(uname -sr)"

    echo "## Environment"
    echo
    echo "- arch: \`${HOST_ARCH}\`${container_tag}"
    echo "- cpu: ${model}"
    echo "- logical cores: ${cores}"
    echo "- caches: L1d $(cache_size_at 0) / L1i $(cache_size_at 1) / L2 $(cache_size_at 2) / L3 $(cache_size_at 3)"
    echo "- kernel: ${kernel}"
    echo

    labels=""
    paths=""
    for tok in $INTERPS; do
        labels="$labels ${tok%%=*}"
        paths="$paths ${tok#*=}"
    done

    # Header row.
    printf '|'
    for l in "" $labels; do printf ' %s |' "$l"; done
    echo
    printf '|---'
    for _ in $labels; do printf '|---'; done
    echo '|'

    # Executable row.
    printf '| executable |'
    for p in $paths; do printf ' `%s` |' "$p"; done
    echo

    # Version row.
    printf '| version |'
    for p in $paths; do
        printf ' %s |' "$($p -c 'import sys; print(sys.version.split()[0])')"
    done
    echo

    # Linkage row.
    printf '| linkage |'
    for p in $paths; do printf ' %s |' "$(linkage_of "$p")"; done
    echo

    # Size on disk row.
    printf '| size on disk |'
    for p in $paths; do printf ' %s |' "$(size_label_for "$p")"; done
    echo
} > "$TMP/report.md"

# ---------------------------------------------------------------------------
# Step 2: run the CPU micro-benchmarks and the startup probe for each interp.
# ---------------------------------------------------------------------------

echo "running CPU micro-benchmarks for each interpreter..." >&2
for tok in $INTERPS; do
    label="${tok%%=*}"
    path="${tok#*=}"
    "$path" "$HERE/microbench.py" > "$TMP/cpu_${label}.json"
done

echo "running startup probe (${ITERS} iters per scenario, each interpreter)..." >&2
# Use the system python (or first available) as the host harness so timings
# of the candidate interpreters are independent of which one we measure.
HARNESS="$SYSTEM"
[ -x "$HARNESS" ] || HARNESS="$STATIC"
for tok in $INTERPS; do
    label="${tok%%=*}"
    path="${tok#*=}"
    "$HARNESS" "$HERE/measure_startup.py" "$path" "$ITERS" > "$TMP/start_${label}.json"
done

# ---------------------------------------------------------------------------
# Step 3: render comparison tables (CPU + startup) with a geomean row each.
# ---------------------------------------------------------------------------

# Build the args list "<label> <cpu.json> <start.json>" for the renderer.
RENDER_ARGS=""
for tok in $INTERPS; do
    label="${tok%%=*}"
    RENDER_ARGS="$RENDER_ARGS $label $TMP/cpu_${label}.json $TMP/start_${label}.json"
done

"$HARNESS" - $RENDER_ARGS <<'PY' >> "$TMP/report.md"
import json
import math
import sys


def geomean(values):
    # All inputs are positive measured times / ratios, so log is well-defined.
    return math.exp(sum(math.log(v) for v in values) / len(values))


def fmt_ns(ns):
    if ns >= 1_000_000:
        return f"{ns/1_000_000:.2f} ms"
    if ns >= 1_000:
        return f"{ns/1_000:.2f} us"
    return f"{ns:.1f} ns"


def fmt_ms(ms):
    return f"{ms:.2f} ms"


# argv: [label1, cpu1, start1, label2, cpu2, start2, ...]
trip = sys.argv[1:]
assert len(trip) % 3 == 0 and trip, "renderer needs (label, cpu, start) triples"
labels = trip[0::3]
cpu_paths = trip[1::3]
start_paths = trip[2::3]

cpu = {l: json.load(open(p)) for l, p in zip(labels, cpu_paths)}
start = {l: json.load(open(p)) for l, p in zip(labels, start_paths)}

baseline = labels[0]  # always "static" by construction in run.sh
others = labels[1:]
bench_names = list(cpu[baseline]["results"].keys())
scenario_names = list(start[baseline]["scenarios"].keys())

# Ratio convention: report X / static, so > 1 means X took longer than the
# static baseline (i.e. the static build is that-many-times faster). The
# geomean row aggregates those ratios; > 1 means the static build wins on
# average across the suite.

# CPU table -----------------------------------------------------------------
print()
print("## CPU micro-benchmarks (lower ns/op is better)")
print()
print(f"Best of 7 runs after warmup; values are per inner-loop op. Ratio column")
print(f"is X / {baseline}: > 1 means {baseline} was faster on that row. The final row")
print(f"is the geometric mean of those ratios.")
print()

header_cells = ["benchmark"] + labels + [f"{l}/{baseline}" for l in others]
print("| " + " | ".join(header_cells) + " |")
print("|---" + "|---:" * (len(header_cells) - 1) + "|")

ratios = {l: [] for l in others}
for name in bench_names:
    row = [name]
    for l in labels:
        row.append(fmt_ns(cpu[l]["results"][name]["ns_per_op"]))
    base = cpu[baseline]["results"][name]["ns_per_op"]
    for l in others:
        other = cpu[l]["results"][name]["ns_per_op"]
        r = other / base if base else float("inf")
        ratios[l].append(r)
        row.append(f"{r:.2f}x")
    print("| " + " | ".join(row) + " |")

geo_row = ["**geomean (X / " + baseline + ")**"] + [""] * len(labels) + [
    f"**{geomean(ratios[l]):.2f}x**" for l in others
]
print("| " + " | ".join(geo_row) + " |")

# Startup table -------------------------------------------------------------
print()
print("## Startup / first-import latency (lower ms is better)")
print()
samples = start[baseline]["scenarios"][scenario_names[0]]["samples"]
print(
    f"Wall-clock spawn time measured externally ({samples} samples each, min)."
    f" Ratio column is X / {baseline}: > 1 means {baseline} spawned faster."
    f" Final row is the geometric mean across scenarios."
)
print()

header_cells = ["scenario"] + labels + [f"{l}/{baseline}" for l in others]
print("| " + " | ".join(header_cells) + " |")
print("|---" + "|---:" * (len(header_cells) - 1) + "|")

start_ratios = {l: [] for l in others}
for name in scenario_names:
    row = [name]
    for l in labels:
        row.append(fmt_ms(start[l]["scenarios"][name]["min_ms"]))
    base = start[baseline]["scenarios"][name]["min_ms"]
    for l in others:
        other = start[l]["scenarios"][name]["min_ms"]
        r = other / base if base else float("inf")
        start_ratios[l].append(r)
        row.append(f"{r:.2f}x")
    print("| " + " | ".join(row) + " |")

geo_row = ["**geomean (X / " + baseline + ")**"] + [""] * len(labels) + [
    f"**{geomean(start_ratios[l]):.2f}x**" for l in others
]
print("| " + " | ".join(geo_row) + " |")
PY

# Trailing placeholder. The intent is that whichever agent (or human) runs
# this script writes their interpretation *into the same file*, replacing
# the italic line below. AGENTS.md spells out what good analysis looks like.
{
    echo
    echo "## Analysis"
    echo
    echo "_To be filled in by whoever ran the benchmark: what moved relative to"
    echo "the previous run, which hypotheses the numbers confirm or falsify, and"
    echo "what to investigate next. See \`AGENTS.md\` for guidance._"
} >> "$TMP/report.md"

# Persist the report under benchmark/reports/ and emit on stdout for ad-hoc
# inspection. The directory is kept under version control so the run history
# is reviewable.
REPORTS_DIR="$ROOT/benchmark/reports"
mkdir -p "$REPORTS_DIR"
REPORT="$REPORTS_DIR/${STAMP}_${HOST_ARCH}.md"
cp "$TMP/report.md" "$REPORT"

# /workspace is bind-mounted from the host inside the container, so a report
# written as container-root would land on disk owned by uid 0 and the host
# user (or an agent running on the host) would lack write permission to
# append analysis. Hand the file back to whichever uid owns the workspace.
if [ "$(id -u 2>/dev/null || echo 0)" = "0" ] && [ -e "$ROOT" ]; then
    host_uid="$(stat -c%u "$ROOT" 2>/dev/null || echo 0)"
    host_gid="$(stat -c%g "$ROOT" 2>/dev/null || echo 0)"
    if [ "$host_uid" != "0" ]; then
        chown "$host_uid:$host_gid" "$REPORT" 2>/dev/null || true
    fi
fi

cat "$TMP/report.md"
echo "saved: ${REPORT#$ROOT/}" >&2
