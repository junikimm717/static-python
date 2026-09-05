#!/usr/bin/env python3
"""Import, list, verify, and publish committed benchmark sessions.

Sessions come from `./staticpy bench` on a real machine. This script never
runs a benchmark; it copies the reviewable files into benchmarks/ and
renders a static site from that tree.
"""

from __future__ import annotations

import argparse
import html
import json
import math
import re
import shutil
import sys
from pathlib import Path

# Must match src/staticpy/internal/bench/protocol.go.
CURRENT_PROTOCOL = 2
INDEX_VERSION = 1

ALLOWED_FILES = (
    "manifest.json",
    "env.json",
    "report.json",
    "report.md",
    "report.html",
    "skipped.json",
    "timeline.jsonl",
)
REQUIRED_FILES = ("manifest.json", "report.json")
STAMP_ARCH = re.compile(r"^(\d{8}T\d{6}Z)-(.+)$")
SAFE_ID = re.compile(r"^[A-Za-z0-9._-]+$")


class ManagerError(Exception):
    """User-facing failure; the CLI prints this and exits 1."""


def repo_root(explicit: Path | None = None) -> Path:
    if explicit is not None:
        return explicit.resolve()
    return Path(__file__).resolve().parent


def archive_dir(root: Path) -> Path:
    return root / "benchmarks"


def fixtures_dir(root: Path) -> Path:
    return archive_dir(root) / "fixtures"


def index_path(root: Path) -> Path:
    return archive_dir(root) / "index.json"


def _load_json(path: Path):
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as e:
        raise ManagerError(f"{path}: {e}") from e
    try:
        return json.loads(text)
    except json.JSONDecodeError as e:
        raise ManagerError(f"{path}: invalid JSON: {e}") from e


def _write_json(path: Path, obj) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    data = json.dumps(obj, indent=2, sort_keys=False) + "\n"
    tmp = path.with_name(path.name + ".tmp")
    tmp.write_text(data, encoding="utf-8")
    tmp.replace(path)


def _is_int(v) -> bool:
    return isinstance(v, int) and not isinstance(v, bool)


def parse_id(name: str) -> tuple[str, str]:
    m = STAMP_ARCH.match(name)
    if m:
        return m.group(1), m.group(2)
    return "", ""


def _session_id(session: Path) -> str:
    name = session.name
    if not name or name in (".", "..") or not SAFE_ID.match(name):
        raise ManagerError(f"refusing destination name {name!r}")
    if name in ("fixtures", "index.json", "README.md"):
        raise ManagerError(f"refusing destination name {name!r}")
    return name


def _protocol(manifest) -> int | None:
    if not isinstance(manifest, dict) or "protocol" not in manifest:
        return None
    p = manifest["protocol"]
    if not _is_int(p):
        return None
    return p


def _skipped_count(run_dir: Path, manifest: dict) -> int:
    skipped_path = run_dir / "skipped.json"
    if skipped_path.is_file():
        data = _load_json(skipped_path)
        if isinstance(data, list):
            return len(data)
    skipped = manifest.get("skipped")
    if isinstance(skipped, list):
        return len(skipped)
    return 0


def _arms(manifest: dict, report: dict) -> list[str]:
    arms: list[str] = []
    seen = set()
    interps = manifest.get("interpreters")
    if isinstance(interps, list):
        for item in interps:
            if isinstance(item, dict):
                label = item.get("label")
                if isinstance(label, str) and label not in seen:
                    arms.append(label)
                    seen.add(label)
    if not arms:
        baseline = report.get("baseline")
        geo = report.get("geomean_vs_baseline")
        if isinstance(baseline, str):
            arms.append(baseline)
            seen.add(baseline)
        if isinstance(geo, dict):
            for k in geo:
                if k not in seen:
                    arms.append(k)
                    seen.add(k)
    return arms


def _machine_subset(env) -> dict:
    if not isinstance(env, dict):
        return {"kernel": "", "cpu_model": "", "memory_bytes": 0}
    mem = env.get("memory_bytes", 0)
    if not _is_int(mem):
        mem = 0
    return {
        "kernel": env.get("kernel", "") if isinstance(env.get("kernel"), str) else "",
        "cpu_model": env.get("cpu_model", "") if isinstance(env.get("cpu_model"), str) else "",
        "memory_bytes": mem,
    }


def _iter_run_dirs(root: Path) -> list[tuple[Path, bool]]:
    """(path, is_fixture) for every session directory that has a manifest."""
    found: list[tuple[Path, bool]] = []
    base = archive_dir(root)
    if not base.is_dir():
        return found
    fx = fixtures_dir(root)
    if fx.is_dir():
        for child in sorted(fx.iterdir()):
            if child.is_dir() and (child / "manifest.json").is_file():
                found.append((child, True))
    for child in sorted(base.iterdir()):
        if not child.is_dir() or child.name == "fixtures":
            continue
        if (child / "manifest.json").is_file():
            found.append((child, False))
    return found


def load_run(run_dir: Path, *, fixture: bool) -> dict:
    manifest = _load_json(run_dir / "manifest.json")
    if not isinstance(manifest, dict):
        raise ManagerError(f"{run_dir / 'manifest.json'}: expected an object")
    report_path = run_dir / "report.json"
    if not report_path.is_file():
        raise ManagerError(f"{run_dir}: missing report.json")
    report = _load_json(report_path)
    if not isinstance(report, dict):
        raise ManagerError(f"{report_path}: expected an object")
    env = {}
    env_path = run_dir / "env.json"
    if env_path.is_file():
        env = _load_json(env_path)
        if not isinstance(env, dict):
            raise ManagerError(f"{env_path}: expected an object")
    protocol = _protocol(manifest)
    if protocol is None:
        # Keep a numeric field in the index; verify() rejects the non-int.
        protocol_out = manifest.get("protocol", 0)
        if not _is_int(protocol_out):
            protocol_out = 0
        protocol = protocol_out
    stamp, arch = parse_id(run_dir.name)
    if not stamp:
        stamp = manifest.get("stamp", "") if isinstance(manifest.get("stamp"), str) else ""
    geo = report.get("geomean_vs_baseline")
    if not isinstance(geo, dict):
        geo = {}
    suite = manifest.get("suite")
    if not isinstance(suite, dict):
        suite = {}
    baseline = ""
    if isinstance(manifest.get("baseline"), str):
        baseline = manifest["baseline"]
    elif isinstance(report.get("baseline"), str):
        baseline = report["baseline"]
    return {
        "id": run_dir.name,
        "stamp": stamp,
        "arch": arch,
        "protocol": protocol,
        "baseline": baseline,
        "arms": _arms(manifest, report),
        "geomean_vs_baseline": geo,
        "skipped": _skipped_count(run_dir, manifest),
        "suite": suite,
        "machine": _machine_subset(env),
        "stale_protocol": protocol != CURRENT_PROTOCOL,
        "fixture": fixture,
        "path": str(run_dir),
        "_manifest": manifest,
        "_report": report,
        "_env": env,
    }


def scan_runs(root: Path) -> list[dict]:
    runs = [load_run(path, fixture=fixture) for path, fixture in _iter_run_dirs(root)]

    def newest(r):
        return (r["stamp"], r["id"])

    prod = sorted((r for r in runs if not r["fixture"]), key=newest, reverse=True)
    fx = sorted((r for r in runs if r["fixture"]), key=newest, reverse=True)
    return prod + fx


def index_entry(run: dict) -> dict:
    entry = {
        "id": run["id"],
        "stamp": run["stamp"],
        "arch": run["arch"],
        "protocol": run["protocol"],
        "baseline": run["baseline"],
        "arms": run["arms"],
        "geomean_vs_baseline": run["geomean_vs_baseline"],
        "skipped": run["skipped"],
        "suite": run["suite"],
        "machine": run["machine"],
        "stale_protocol": run["stale_protocol"],
        "fixture": run["fixture"],
    }
    rev = (run.get("_manifest") or {}).get("git_revision")
    if isinstance(rev, str) and rev:
        entry["git_revision"] = rev
    return entry


def refresh_index(root: Path) -> dict:
    payload = {
        "index_version": INDEX_VERSION,
        "runs": [index_entry(r) for r in scan_runs(root)],
    }
    _write_json(index_path(root), payload)
    return payload


def find_run(root: Path, name: str) -> dict:
    name = name.rstrip("/")
    if name.startswith("fixtures/"):
        name = name.split("/", 1)[1]
    name = Path(name).name
    for path, fixture in _iter_run_dirs(root):
        if path.name == name:
            return load_run(path, fixture=fixture)
    raise ManagerError(f"no imported run named {name!r}")


def import_session(
    root: Path,
    session: Path,
    *,
    force: bool = False,
    allow_stale: bool = False,
) -> Path:
    session = session.resolve()
    if not session.is_dir():
        raise ManagerError(f"{session} is not a directory")
    dest_id = _session_id(session)
    for req in REQUIRED_FILES:
        if not (session / req).is_file():
            raise ManagerError(f"{session}: missing {req}")
    manifest = _load_json(session / "manifest.json")
    if not isinstance(manifest, dict):
        raise ManagerError(f"{session / 'manifest.json'}: expected an object")
    protocol = _protocol(manifest)
    if protocol is None:
        raise ManagerError(
            f"{session}: manifest protocol must be an int (got {manifest.get('protocol')!r})"
        )
    if protocol != CURRENT_PROTOCOL and not allow_stale:
        raise ManagerError(
            f"{session}: protocol {protocol} != {CURRENT_PROTOCOL}; "
            "pass --allow-stale-protocol to import anyway"
        )
    report = _load_json(session / "report.json")
    if not isinstance(report, dict):
        raise ManagerError(f"{session / 'report.json'}: expected an object")
    dest = archive_dir(root) / dest_id
    if dest.exists() and not force:
        raise ManagerError(f"{dest} already exists; pass --force to overwrite")
    archive_dir(root).mkdir(parents=True, exist_ok=True)
    staging = dest.with_name(dest.name + ".staging")
    if staging.exists():
        shutil.rmtree(staging)
    staging.mkdir(parents=True)
    try:
        for name in ALLOWED_FILES:
            src = session / name
            if src.is_file():
                shutil.copy2(src, staging / name)
        if dest.exists():
            shutil.rmtree(dest)
        staging.rename(dest)
    except Exception:
        if staging.exists():
            shutil.rmtree(staging, ignore_errors=True)
        raise
    refresh_index(root)
    return dest


def delete_run(
    root: Path,
    name: str,
    *,
    yes: bool = False,
    fixtures: bool = False,
) -> Path:
    run = find_run(root, name)
    dest = Path(run["path"])
    if run["fixture"] and not fixtures:
        raise ManagerError(
            f"{dest} is under fixtures/; pass --fixtures to delete demo data"
        )
    if not yes:
        if sys.stdin.isatty():
            ans = input(f"delete {dest}? [y/N] ")
            if ans.strip().lower() not in ("y", "yes"):
                raise ManagerError("aborted")
        else:
            raise ManagerError("refusing to delete without --yes")
    shutil.rmtree(dest)
    refresh_index(root)
    return dest


def verify_runs(root: Path) -> int:
    dirs = _iter_run_dirs(root)
    if not dirs:
        # An empty archive is valid: Pages still has the fixture heading once
        # a fixture exists; with neither, verify is still a successful no-op.
        return 0
    for path, fixture in dirs:
        for req in REQUIRED_FILES:
            p = path / req
            if not p.is_file():
                raise ManagerError(f"{path}: missing {req}")
        manifest = _load_json(path / "manifest.json")
        if not isinstance(manifest, dict):
            raise ManagerError(f"{path / 'manifest.json'}: expected an object")
        if not _is_int(manifest.get("protocol")):
            raise ManagerError(
                f"{path / 'manifest.json'}: protocol must be an int, "
                f"got {manifest.get('protocol')!r}"
            )
        report = _load_json(path / "report.json")
        if not isinstance(report, dict):
            raise ManagerError(f"{path / 'report.json'}: expected an object")
        skipped_path = path / "skipped.json"
        if skipped_path.is_file():
            skipped = _load_json(skipped_path)
            if not isinstance(skipped, list):
                raise ManagerError(f"{skipped_path}: skipped.json must be a list")
        else:
            raise ManagerError(f"{path}: missing skipped.json")
        load_run(path, fixture=fixture)
    return len(dirs)


def _esc(s) -> str:
    return html.escape("" if s is None else str(s), quote=True)


def _suite_label(suite) -> str:
    if not isinstance(suite, dict):
        return "-"
    name = suite.get("name")
    if isinstance(name, str) and name:
        return name
    if isinstance(suite.get("pyperformance"), str):
        return "pyperformance"
    return "-"


def _fmt_geo(geo: dict) -> str:
    parts = []
    for k, v in geo.items():
        if isinstance(v, (int, float)) and not isinstance(v, bool):
            parts.append(f"{k}:{v:.3f}")
        else:
            parts.append(f"{k}:{v}")
    return ",".join(parts) if parts else "-"


# Factor *values* from the session protocol, not profile names.
_LTO_WORDS = {
    "whole-graph": "whole-program LTO",
    "per-dep": "per-library LTO",
    "none": "no LTO",
}


def _interp_tip(item: dict, *, baseline: str = "") -> str:
    if not isinstance(item, dict):
        return ""
    label = str(item.get("label") or "")
    factors = item.get("factors") if isinstance(item.get("factors"), dict) else {}
    linkage = factors.get("linkage") or item.get("linkage") or ""
    libc = factors.get("libc") or ""
    who = " ".join(p for p in (linkage, libc) if p)
    if who:
        who = who[0].upper() + who[1:] + " interpreter"
    lto = _LTO_WORDS.get(factors.get("lto", ""), factors.get("lto") or "")
    alloc = factors.get("allocator") or ""
    if alloc and alloc != "mimalloc":
        alloc = alloc + " malloc"
    pgo = factors.get("pgo")
    bits = [p for p in (lto, alloc, "PGO" if pgo is True else ("no PGO" if pgo is False else "")) if p]
    if who and bits:
        head = f"{who} with {', '.join(bits)}"
    else:
        head = who or ", ".join(bits)
    parts = [p for p in (head,) if p]
    if baseline and label == baseline:
        parts.append("Baseline for the ratios")
    key = _short_sha(item.get("artifact_key"))
    if key:
        parts.append("artifact " + key)
    if not parts:
        return label
    text = ". ".join(parts)
    return text if text.endswith(".") else text + "."


def _interp_tips(manifest: dict) -> dict[str, str]:
    tips: dict[str, str] = {}
    interps = manifest.get("interpreters")
    if not isinstance(interps, list):
        return tips
    baseline = manifest.get("baseline") if isinstance(manifest.get("baseline"), str) else ""
    for item in interps:
        if isinstance(item, dict) and item.get("label"):
            tips[str(item["label"])] = _interp_tip(item, baseline=baseline)
    return tips


def _tipped(label: str, tips: dict[str, str]) -> str:
    tip = tips.get(label)
    if not tip:
        return _esc(label)
    return f'<span data-tip="{_esc(tip)}">{_esc(label)}</span>'


def geomean_svg(run: dict) -> str:
    geo = run.get("geomean_vs_baseline") or {}
    baseline = run.get("baseline") or ""
    arms = list(run.get("arms") or [])
    labels = []
    vals = []
    if baseline:
        labels.append(baseline)
        vals.append(1.0)
    for k, v in geo.items():
        if k == baseline:
            continue
        if isinstance(v, (int, float)) and not isinstance(v, bool) and v > 0:
            labels.append(k)
            vals.append(float(v))
    if not labels:
        return ""
    label_w, bar_max, bar_h, gap, left, top = 180, 320, 22, 10, 16, 16
    max_v = max(vals) if vals else 1.0
    width = left + label_w + bar_max + 70
    height = top + len(labels) * (bar_h + gap) + 4
    parts = [
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {width} {height}" '
        f'width="{width}" height="{height}" role="img" '
        f'aria-label="geomean vs {_esc(baseline)}">',
        f'<rect width="{width}" height="{height}" fill="transparent"/>',
    ]
    tips = _interp_tips(run.get("_manifest") or {})
    for i, (label, val) in enumerate(zip(labels, vals)):
        y = top + i * (bar_h + gap)
        bw = (val / max_v * bar_max) if max_v > 0 else 0
        fill = "#4a5568" if label == baseline else "#2b6cb0"
        tip = tips.get(label, "")
        tip_attr = f' data-tip="{_esc(tip)}"' if tip else ""
        parts.append(
            f'<text x="{left + label_w - 8}" y="{y + bar_h - 5}" text-anchor="end" '
            f'font-size="12" fill="currentColor"{tip_attr}>{_esc(label)}</text>'
        )
        parts.append(
            f'<rect x="{left + label_w}" y="{y}" width="{bw:.1f}" height="{bar_h}" '
            f'fill="{fill}" rx="3"/>'
        )
        parts.append(
            f'<text x="{left + label_w + bw + 8:.1f}" y="{y + bar_h - 5}" '
            f'font-size="11" fill="currentColor">{val:.3f}x</text>'
        )
    parts.append("</svg>")
    return "\n".join(parts)


def _body_inner(doc: str) -> str:
    m = re.search(r"<body[^>]*>(.*)</body>", doc, re.I | re.S)
    return m.group(1) if m else doc


CSS = """
body{font:15px/1.5 system-ui,sans-serif;max-width:1100px;margin:2rem auto;padding:0 1.25rem;color:#1a202c}
h1{font-size:1.75rem;font-weight:650;margin:.25rem 0 .4rem}
h2{font-size:1.15rem;font-weight:600;margin:1.75rem 0 .6rem}
h3{font-size:1rem;font-weight:600;margin:1rem 0 .4rem}
a{color:#2b6cb0}
.lede{color:#4a5568;margin:.15rem 0 .4rem}
.meta{color:#4a5568;font-size:.9rem;margin:.15rem 0 1.25rem}
.skip{color:#744210}
.table-wrap{overflow-x:auto;margin:1rem 0}
table{border-collapse:collapse;width:100%;margin:1rem 0}
th,td{border:1px solid #cbd5e0;padding:.35rem .55rem;text-align:right;vertical-align:top}
th:first-child,td:first-child{text-align:left}
table.ratios{font-variant-numeric:tabular-nums}
.ratio{font-weight:600}
.ratio.lose3{background:#d55e00;color:#fff}
.ratio.lose2{background:#e69f00;color:#1a202c}
.ratio.lose1{background:#f0e442;color:#1a202c}
.ratio.win1{background:#56b4e9;color:#1a202c}
.ratio.win2{background:#0072b2;color:#fff}
.ratio.win3{background:#cc79a7;color:#1a202c}
.scale{color:#4a5568;font-size:.9rem;margin:.4rem 0 0}
.scale.swatches{display:flex;gap:.4rem;flex-wrap:wrap;margin:.55rem 0 .3rem;align-items:center}
.scale .swatch{display:inline-block;padding:.2rem .55rem;border:1px solid #cbd5e0;border-radius:4px;font-variant-numeric:tabular-nums}
.scale .swatch:not(.ratio){background:#edf2f7;color:#4a5568}
[data-tip]{cursor:help;border-bottom:1px dotted currentColor}
#arm-tip{position:fixed;z-index:20;max-width:22rem;padding:.55rem .75rem;border-radius:6px;background:#1a202c;color:#f7fafc;font-size:.85rem;line-height:1.4;box-shadow:0 4px 16px rgba(0,0,0,.35);pointer-events:none}
#arm-tip[hidden]{display:none}
.env{background:#f7fafc;padding:.75rem 1rem;border-radius:6px}
.env dt{font-weight:600;float:left;clear:left;width:9rem}
.env dd{margin-left:9.5rem}
.env::after{content:"";display:block;clear:both}
.env pre{white-space:pre-wrap;font-size:.85rem;overflow:auto;margin:.25rem 0;float:none}
.banner{background:#fefcbf;border:1px solid #d69e2e;padding:.75rem 1rem;border-radius:6px;margin:1rem 0}
.empty{background:#edf2f7;padding:.75rem 1rem;border-radius:6px}
.stale{color:#c05621;font-weight:600}
svg{max-width:100%;height:auto}
nav{margin-bottom:1.25rem}
.panel{border:1px solid #e2e8f0;border-radius:8px;padding:.35rem 1rem;margin:1rem 0;background:#fff}
.panel>summary{cursor:pointer;font-weight:600;padding:.45rem 0}
.panel>summary:hover{color:#2b6cb0}
.pkgs{columns:2;margin:.4rem 0 1rem;padding-left:1.2rem}
.pkgs li{break-inside:avoid}
@media (prefers-color-scheme: dark){
body{color:#e2e8f0;background:#1a202c}
a{color:#63b3ed}
.lede,.meta{color:#a0aec0}
.skip{color:#f6ad55}
th,td{border-color:#4a5568}
.ratio.lose3{background:#d55e00;color:#fff}
.ratio.lose2{background:#e69f00;color:#1a202c}
.ratio.lose1{background:#b7791f;color:#1a202c}
.ratio.win1{background:#56b4e9;color:#1a202c}
.ratio.win2{background:#0072b2;color:#fff}
.ratio.win3{background:#cc79a7;color:#1a202c}
.scale{color:#a0aec0}
.scale .swatch{border-color:#4a5568}
.scale .swatch:not(.ratio){background:#2d3748;color:#a0aec0}
.env,.empty,.panel{background:#2d3748;border-color:#4a5568}
.banner{background:#744210;border-color:#d69e2e;color:#fefcbf}
#arm-tip{background:#f7fafc;color:#1a202c}
}
"""


_TIP_JS = """
<script>
(function(){
  var box = document.getElementById("arm-tip");
  if (!box) return;
  function hide(){ box.hidden = true; }
  document.addEventListener("pointerover", function(e){
    var t = e.target.closest("[data-tip]");
    if (!t) { hide(); return; }
    box.textContent = t.getAttribute("data-tip");
    box.hidden = false;
    var r = t.getBoundingClientRect();
    var x = Math.min(r.left, innerWidth - box.offsetWidth - 8);
    var y = r.bottom + 8;
    if (y + box.offsetHeight > innerHeight - 8) y = r.top - box.offsetHeight - 8;
    box.style.left = Math.max(8, x) + "px";
    box.style.top = Math.max(8, y) + "px";
  });
  document.addEventListener("pointerout", function(e){
    if (!e.relatedTarget || !e.relatedTarget.closest("[data-tip]")) hide();
  });
})();
</script>
"""


def _page(title: str, body: str) -> str:
    return (
        "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n"
        "<meta charset=\"utf-8\">\n"
        "<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n"
        f"<title>{_esc(title)}</title>\n"
        f"<style>{CSS}</style>\n</head>\n<body>\n{body}\n"
        '<div id="arm-tip" hidden></div>\n'
        f"{_TIP_JS}</body>\n</html>\n"
    )


def _headline_geo(run: dict) -> str:
    geo = run.get("geomean_vs_baseline") or {}
    baseline = run.get("baseline") or ""
    for key in ("default",):
        v = geo.get(key)
        if isinstance(v, (int, float)) and not isinstance(v, bool):
            return f"{key} {v:.3f}x"
    for key, v in geo.items():
        if key == baseline:
            continue
        if isinstance(v, (int, float)) and not isinstance(v, bool):
            return f"{key} {v:.3f}x"
    return "—"


def _run_table(runs: list[dict], *, rel_prefix: str) -> str:
    if not runs:
        return ""
    parts = [
        "<table>\n<thead><tr>"
        "<th>id</th><th>suite</th><th>baseline</th>"
        "<th>headline vs baseline</th><th>skipped</th><th>cpu</th>"
        "</tr></thead>\n<tbody>\n"
    ]
    for r in runs:
        stale = ' <span class="stale">stale protocol</span>' if r["stale_protocol"] else ""
        href = f"{rel_prefix}run/{_esc(r['id'])}/index.html"
        cpu = (r.get("machine") or {}).get("cpu_model") or ""
        parts.append(
            f'<tr><td><a href="{href}">{_esc(r["id"])}</a>{stale}</td>'
            f'<td>{_esc(_suite_label(r.get("suite")))}</td>'
            f'<td>{_esc(r["baseline"])}</td>'
            f'<td>{_esc(_headline_geo(r))}</td>'
            f'<td>{_esc(r["skipped"])}</td>'
            f'<td>{_esc(cpu)}</td></tr>\n'
        )
    parts.append("</tbody></table>\n")
    return "".join(parts)


def _env_dl(env: dict) -> str:
    if not env:
        return "<p>no env.json</p>\n"
    keys = [
        "kernel", "cpu_model", "logical_cores", "memory", "memory_bytes",
        "memory_available", "cache_l1d", "cache_l1i", "cache_l2", "cache_l3",
        "topology", "affinity", "container",
    ]
    parts = ['<dl class="env">\n']
    for k in keys:
        if k in env:
            parts.append(f"<dt>{_esc(k)}</dt><dd>{_format_env_value(env[k])}</dd>\n")
    parts.append("</dl>\n")
    return "".join(parts)


def _fingerprint_html(fp: dict) -> str:
    ident = dict(fp)
    tel = ident.pop("telemetry", None)
    parts = [
        "<h3>Fingerprint (identity)</h3>\n",
        "<p>Hashed into <code>fingerprint_sha256</code>. Load, current MHz, "
        "free RAM, and this run's pin live under telemetry and are not in "
        "the digest.</p>\n",
        f'<div class="env"><pre>{_esc(json.dumps(ident, indent=2, sort_keys=True))}</pre></div>\n',
    ]
    if tel is not None:
        parts.append("<h3>Telemetry</h3>\n")
        parts.append(
            "<p>Point-in-time host snapshot. Recorded for audit, not hashed.</p>\n"
        )
        parts.append(
            f'<div class="env"><pre>{_esc(json.dumps(tel, indent=2, sort_keys=True))}</pre></div>\n'
        )
    return "".join(parts)


def _interpreters_html(manifest: dict) -> str:
    interps = manifest.get("interpreters")
    if not isinstance(interps, list) or not interps:
        return ""
    parts = [
        '<details class="panel"><summary>Interpreters</summary>\n',
        "<p>Profile names are not a stable description of the binary; "
        "factors and the content-addressed artifact key are.</p>\n",
        '<div class="table-wrap"><table>\n<thead><tr>'
        "<th>label</th><th>binary_sha256</th><th>artifact_key</th>"
        "<th>linkage</th><th>libc</th><th>lto</th><th>allocator</th>"
        "<th>pgo</th><th>toolchain</th>"
        "</tr></thead>\n<tbody>\n",
    ]
    for item in interps:
        if not isinstance(item, dict):
            continue
        factors = item.get("factors") if isinstance(item.get("factors"), dict) else {}
        label = str(item.get("label", ""))
        parts.append(
            "<tr>"
            f'<td>{_tipped(label, {label: _interp_tip(item)})}</td>'
            f'<td><code>{_esc(_short_sha(item.get("binary_sha256") or item.get("sha256")))}</code></td>'
            f'<td><code>{_esc(_short_sha(item.get("artifact_key")))}</code></td>'
            f'<td>{_esc(factors.get("linkage", item.get("linkage", "")))}</td>'
            f'<td>{_esc(factors.get("libc", ""))}</td>'
            f'<td>{_esc(factors.get("lto", ""))}</td>'
            f'<td>{_esc(factors.get("allocator", ""))}</td>'
            f'<td>{_esc(factors.get("pgo", ""))}</td>'
            f'<td>{_esc(factors.get("toolchain", ""))}</td>'
            "</tr>\n"
        )
    parts.append("</tbody></table></div>\n</details>\n")
    return "".join(parts)


# Discrete bands. A one-hue opacity ramp made every win look the same.
def _ratio_band(v: float) -> str:
    if v <= 0 or math.isnan(v) or math.isinf(v):
        return ""
    if v < 0.75:
        return "lose3"
    if v < 0.90:
        return "lose2"
    if v < 0.98:
        return "lose1"
    if v <= 1.02:
        return ""
    if v < 1.20:
        return "win1"
    if v < 1.50:
        return "win2"
    return "win3"


def _ratio_td(v: float) -> str:
    band = _ratio_band(v)
    if band:
        return f'<td class="ratio {band}">{v:.2f}x</td>'
    return f"<td>{v:.2f}x</td>"


def _ratio_scale_html() -> str:
    stops = (
        (0.5, "<0.75×"),
        (0.8, "0.75–0.90×"),
        (0.95, "0.90–0.98×"),
        (1.0, "±2%"),
        (1.10, "1.02–1.20×"),
        (1.35, "1.20–1.50×"),
        (2.0, "≥1.50×"),
    )
    chips = []
    for v, label in stops:
        band = _ratio_band(v)
        cls = f"swatch ratio {band}".strip() if band else "swatch"
        chips.append(f'<span class="{cls}">{_esc(label)}</span>')
    return (
        '<p class="scale">Sky = small, blue = large, magenta = outlier. '
        "Yellow / orange / vermillion are the same steps slower.</p>\n"
        f'<p class="scale swatches">{"".join(chips)}</p>\n'
    )


def _ratio_table_html(report: dict, order: list[str], tips: dict[str, str] | None = None) -> str:
    rows = report.get("rows")
    if not isinstance(rows, list) or not rows or not order:
        return ""
    tips = tips or {}
    parts = [
        _ratio_scale_html(),
        '<p class="scale">Hover an arm name for what that binary is.</p>\n',
        '<div class="table-wrap"><table class="ratios">\n<thead><tr><th>benchmark</th>',
    ]
    for a in order:
        parts.append(f"<th>{_tipped(a, tips)}</th>")
    parts.append("</tr></thead>\n<tbody>\n")
    for row in rows:
        if not isinstance(row, dict):
            continue
        name = row.get("benchmark", "")
        ratios = row.get("ratio_vs_baseline")
        if not isinstance(ratios, dict):
            ratios = {}
        parts.append(f"<tr><td>{_esc(name)}</td>")
        for a in order:
            v = ratios.get(a)
            if isinstance(v, (int, float)) and not isinstance(v, bool):
                parts.append(_ratio_td(float(v)))
            else:
                parts.append("<td>-</td>")
        parts.append("</tr>\n")
    parts.append("</tbody></table></div>\n")
    return "".join(parts)


def _packages_html(manifest: dict) -> str:
    interps = manifest.get("interpreters")
    if not isinstance(interps, list) or not interps:
        return ""
    groups: list[tuple[list[str], dict]] = []
    for item in interps:
        if not isinstance(item, dict):
            continue
        pkgs = item.get("packages") if isinstance(item.get("packages"), dict) else {}
        if not pkgs:
            continue
        label = item.get("label", "")
        key = tuple(sorted((str(k), str(v)) for k, v in pkgs.items()))
        for labels, existing in groups:
            if tuple(sorted((str(k), str(v)) for k, v in existing.items())) == key:
                labels.append(str(label))
                break
        else:
            groups.append(([str(label)], dict(pkgs)))
    if not groups:
        return ""
    parts = [
        '<details class="panel"><summary>Dependencies</summary>\n',
        "<p>Packages installed into each arm's venv after the suite setup. "
        "Identical sets are shown once.</p>\n",
    ]
    for labels, pkgs in groups:
        heading = ", ".join(labels)
        parts.append(f"<h3>{_esc(heading)}</h3>\n<ul class=\"pkgs\">\n")
        for name, ver in sorted(pkgs.items(), key=lambda kv: str(kv[0]).lower()):
            parts.append(f"<li><code>{_esc(name)}=={_esc(ver)}</code></li>\n")
        parts.append("</ul>\n")
    parts.append("</details>\n")
    return "".join(parts)


def _machine_html(env: dict) -> str:
    fp = env.get("fingerprint") if isinstance(env.get("fingerprint"), dict) else None
    parts = [
        '<details class="panel"><summary>Machine</summary>\n',
        _env_dl(env),
    ]
    if fp is not None:
        parts.append(
            '<details class="panel"><summary>Fingerprint and telemetry</summary>\n'
        )
        parts.append(_fingerprint_html(fp))
        parts.append("</details>\n")
    parts.append("</details>\n")
    return "".join(parts)


def _skipped_html(run: dict) -> str:
    run_dir = Path(run["path"])
    skipped_path = run_dir / "skipped.json"
    items: list = []
    if skipped_path.is_file():
        data = _load_json(skipped_path)
        if isinstance(data, list):
            items = data
    if not items:
        skipped = (run.get("_manifest") or {}).get("skipped")
        if isinstance(skipped, list):
            items = skipped
    n = len(items)
    if n == 0 and not run.get("skipped"):
        return ""
    parts = [
        f'<details class="panel"><summary>Skipped ({n})</summary>\n',
        "<p>Named drops. They do not enter the geomean.</p>\n",
    ]
    if items:
        parts.append("<ul>\n")
        for item in items:
            parts.append(f"<li>{_esc(item)}</li>\n")
        parts.append("</ul>\n")
    parts.append("</details>\n")
    return "".join(parts)


def _run_page_body(run: dict) -> str:
    manifest = run.get("_manifest") or {}
    report = run.get("_report") or {}
    env = run.get("_env") or {}
    suite = _suite_label(run.get("suite"))
    title = f"{suite} comparison" if suite != "-" else run["id"]
    baseline = run.get("baseline") or ""
    cpu = (run.get("machine") or {}).get("cpu_model") or ""
    lede = [run["id"]]
    if cpu:
        lede.append(cpu)
    if baseline:
        lede.append(f"baseline {baseline}")

    parts = [
        '<nav><a href="../../index.html">all runs</a></nav>\n',
    ]
    if run["fixture"]:
        parts.append(
            '<div class="banner"><p><strong>Fixture / demo.</strong> '
            "Not a published score.</p></div>\n"
        )
    if run["stale_protocol"]:
        parts.append(
            f'<p class="stale">stale protocol {run["protocol"]} '
            f"(current is {CURRENT_PROTOCOL})</p>\n"
        )
    parts.append(f"<h1>{_esc(title)}</h1>\n")
    parts.append(f'<p class="lede">{_esc(" · ".join(lede))}</p>\n')
    rev = manifest.get("git_revision")
    if isinstance(rev, str) and rev:
        parts.append(
            f'<p class="meta">git_revision of the <code>staticpy</code> '
            f"executable: <code>{_esc(rev)}</code></p>\n"
        )
    svg = geomean_svg(run)
    if svg:
        heading = f"Geomean vs {baseline}" if baseline else "Geomean vs baseline"
        parts.append(f"<h2>{_esc(heading)}</h2>\n{svg}\n")
    skipped = run.get("skipped") or 0
    if skipped:
        parts.append(
            f'<p class="skip">{_esc(skipped)} benchmarks omitted from the '
            "table; see <code>skipped.json</code>.</p>\n"
        )
    ratio_heading = (
        f"Per-benchmark ratio vs {baseline}" if baseline else "Per-benchmark ratio"
    )
    parts.append(f"<h2>{_esc(ratio_heading)}</h2>\n")
    table = _ratio_table_html(report, run.get("arms") or [], _interp_tips(manifest))
    if table:
        parts.append(table)
    else:
        report_html = Path(run["path"]) / "report.html"
        if report_html.is_file():
            parts.append(_body_inner(report_html.read_text(encoding="utf-8")))
    parts.append(_interpreters_html(manifest))
    parts.append(_packages_html(manifest))
    parts.append(_machine_html(env))
    parts.append(_skipped_html(run))
    return "".join(parts)


def _short_sha(v) -> str:
    if not isinstance(v, str) or not v:
        return ""
    return v[:12] if len(v) > 12 else v


def _format_env_value(v) -> str:
    if isinstance(v, (dict, list)):
        return "<pre>" + _esc(json.dumps(v, indent=2, sort_keys=True)) + "</pre>"
    return _esc(v)


def write_site(root: Path, out: Path) -> Path:
    out = out.resolve()
    runs = scan_runs(root)
    prod = [r for r in runs if not r["fixture"]]
    fx = [r for r in runs if r["fixture"]]
    payload = {
        "index_version": INDEX_VERSION,
        "runs": [index_entry(r) for r in runs],
    }

    if out.exists():
        shutil.rmtree(out)
    (out / "run").mkdir(parents=True)
    (out / "data").mkdir(parents=True)
    _write_json(out / "data" / "index.json", payload)

    body = ["<h1>static-python benchmarks</h1>\n"]
    body.append(
        "<p>These pages are built from the committed <code>benchmarks/</code> "
        "tree. CI does not run <code>./staticpy bench</code>.</p>\n"
    )
    if not prod:
        body.append(
            '<div class="empty"><p><strong>No production runs imported.</strong> '
            "A session lands here with <code>./manage_benchmarks.py import "
            "dist/bench/&lt;stamp&gt;-&lt;arch&gt;</code> after a native "
            "<code>./staticpy bench</code> on a real machine. The numbers "
            "below, if any, are synthetic fixtures — not published scores.</p></div>\n"
        )
    else:
        body.append("<h2>Runs</h2>\n")
        body.append(_run_table(prod, rel_prefix=""))
        for r in prod:
            svg = geomean_svg(r)
            if svg:
                body.append(f'<h3>{_esc(r["id"])}</h3>\n{svg}\n')
    if fx:
        body.append("<h2>Fixture / demo</h2>\n")
        body.append(
            '<div class="banner"><p>Synthetic demo data. The cpu model contains '
            "<code>FIXTURE</code> and the stamp is <code>20000101T…</code>. "
            "These ratios are not scores.</p></div>\n"
        )
        body.append(_run_table(fx, rel_prefix=""))
        for r in fx:
            svg = geomean_svg(r)
            if svg:
                body.append(svg + "\n")
    (out / "index.html").write_text(_page("static-python benchmarks", "".join(body)), encoding="utf-8")

    for r in runs:
        run_out = out / "run" / r["id"]
        run_out.mkdir(parents=True, exist_ok=True)
        (run_out / "index.html").write_text(
            _page(r["id"], _run_page_body(r)), encoding="utf-8"
        )
    return out


def cmd_list(root: Path) -> None:
    runs = scan_runs(root)
    if not runs:
        return
    for r in runs:
        bits = [
            r["id"],
            f"stamp={r['stamp'] or '-'}",
            f"arch={r['arch'] or '-'}",
            f"protocol={r['protocol']}",
            f"suite={_suite_label(r.get('suite'))}",
            f"baseline={r['baseline'] or '-'}",
            f"arms={','.join(r['arms']) or '-'}",
            f"geomean={_fmt_geo(r['geomean_vs_baseline'])}",
            f"skipped={r['skipped']}",
            "stale" if r["stale_protocol"] else "current",
        ]
        if r["fixture"]:
            bits.append("fixture")
        print(" ".join(bits))


def cmd_show(root: Path, name: str) -> None:
    run = find_run(root, name)
    print("# manifest")
    print(json.dumps(run["_manifest"], indent=2))
    print("\n# env")
    print(json.dumps(run["_env"], indent=2))
    print("\n# geomean_vs_baseline")
    print(json.dumps(run["geomean_vs_baseline"], indent=2))


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        prog="manage_benchmarks.py",
        description=(
            "Import bench sessions into benchmarks/, list/verify them, and "
            "render a static GitHub Pages site. Does not run ./staticpy."
        ),
    )
    p.add_argument(
        "--root",
        type=Path,
        default=None,
        help="repository root (default: directory of this script)",
    )
    sub = p.add_subparsers(dest="cmd")

    imp = sub.add_parser("import", help="copy a session directory into benchmarks/")
    imp.add_argument("session", type=Path, help="dist/bench/<stamp>-<arch> or any session dir")
    imp.add_argument("--force", action="store_true", help="overwrite an existing destination")
    imp.add_argument(
        "--allow-stale-protocol",
        action="store_true",
        help=f"import even when protocol != {CURRENT_PROTOCOL}",
    )

    sub.add_parser("list", help="one line per imported run")

    sh = sub.add_parser("show", help="print manifest, env, and geomean for one run")
    sh.add_argument("name")

    dl = sub.add_parser("delete", help="remove an imported run and refresh the index")
    dl.add_argument("name")
    dl.add_argument("--yes", action="store_true", help="do not prompt")
    dl.add_argument(
        "--fixtures",
        action="store_true",
        help="allow deleting under benchmarks/fixtures/",
    )

    sub.add_parser("verify", help="check every imported run, including fixtures")

    site = sub.add_parser("site", help="write a self-contained static site")
    site.add_argument("--out", default="site", help="output directory (default: site)")
    return p


def dispatch(args: argparse.Namespace) -> int:
    root = repo_root(args.root)
    cmd = args.cmd
    if cmd is None:
        build_parser().print_help()
        return 0
    if cmd == "import":
        dest = import_session(
            root, args.session, force=args.force, allow_stale=args.allow_stale_protocol
        )
        print(dest)
        return 0
    if cmd == "list":
        cmd_list(root)
        return 0
    if cmd == "show":
        cmd_show(root, args.name)
        return 0
    if cmd == "delete":
        dest = delete_run(root, args.name, yes=args.yes, fixtures=args.fixtures)
        print(f"deleted {dest}")
        return 0
    if cmd == "verify":
        n = verify_runs(root)
        print(f"verify: {n} run{'s' if n != 1 else ''} ok")
        return 0
    if cmd == "site":
        out = Path(args.out)
        if not out.is_absolute():
            out = Path.cwd() / out
        dest = write_site(root, out)
        print(dest)
        return 0
    raise ManagerError(f"unknown command {cmd!r}")


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    try:
        args = parser.parse_args(argv)
        return dispatch(args)
    except ManagerError as e:
        print(f"error: {e}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
