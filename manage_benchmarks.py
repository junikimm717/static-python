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
import re
import shutil
import sys
from pathlib import Path

# Must match src/staticpy/internal/bench/protocol.go.
CURRENT_PROTOCOL = 1
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
    return {
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


def _fmt_geo(geo: dict) -> str:
    parts = []
    for k, v in geo.items():
        if isinstance(v, (int, float)) and not isinstance(v, bool):
            parts.append(f"{k}:{v:.3f}")
        else:
            parts.append(f"{k}:{v}")
    return ",".join(parts) if parts else "-"


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
    label_w, bar_max, bar_h, gap, left, top = 140, 280, 18, 8, 12, 10
    max_v = max(vals) if vals else 1.0
    width = left + label_w + bar_max + 70
    height = top + len(labels) * (bar_h + gap) + 4
    parts = [
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {width} {height}" '
        f'width="{width}" height="{height}" role="img" '
        f'aria-label="geomean vs {_esc(baseline)}">',
        f'<rect width="{width}" height="{height}" fill="#fff"/>',
    ]
    for i, (label, val) in enumerate(zip(labels, vals)):
        y = top + i * (bar_h + gap)
        bw = (val / max_v * bar_max) if max_v > 0 else 0
        fill = "#4a5568" if label == baseline else "#2b6cb0"
        parts.append(
            f'<text x="{left + label_w - 8}" y="{y + bar_h - 5}" text-anchor="end" '
            f'font-size="12" fill="#1a202c">{_esc(label)}</text>'
        )
        parts.append(
            f'<rect x="{left + label_w}" y="{y}" width="{bw:.1f}" height="{bar_h}" '
            f'fill="{fill}" rx="3"/>'
        )
        parts.append(
            f'<text x="{left + label_w + bw + 8:.1f}" y="{y + bar_h - 5}" '
            f'font-size="11" fill="#1a202c">{val:.3f}x</text>'
        )
    parts.append("</svg>")
    return "\n".join(parts)


def _body_inner(doc: str) -> str:
    m = re.search(r"<body[^>]*>(.*)</body>", doc, re.I | re.S)
    return m.group(1) if m else doc


CSS = """
body{font:14px/1.45 system-ui,sans-serif;max-width:960px;margin:2rem auto;padding:0 1rem;color:#1a202c}
h1,h2{font-weight:600}
a{color:#2b6cb0}
table{border-collapse:collapse;width:100%;margin:1rem 0}
th,td{border:1px solid #cbd5e0;padding:.35rem .6rem;text-align:right}
th:first-child,td:first-child{text-align:left}
.env{background:#f7fafc;padding:.75rem 1rem;border-radius:6px}
.banner{background:#fefcbf;border:1px solid #d69e2e;padding:.75rem 1rem;border-radius:6px;margin:1rem 0}
.empty{background:#edf2f7;padding:.75rem 1rem;border-radius:6px}
.stale{color:#c05621;font-weight:600}
svg{max-width:100%}
nav{margin-bottom:1rem}
"""


def _page(title: str, body: str) -> str:
    return (
        "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n"
        "<meta charset=\"utf-8\">\n"
        "<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n"
        f"<title>{_esc(title)}</title>\n"
        f"<style>{CSS}</style>\n</head>\n<body>\n{body}\n</body>\n</html>\n"
    )


def _run_table(runs: list[dict], *, rel_prefix: str) -> str:
    if not runs:
        return ""
    parts = [
        "<table>\n<thead><tr>"
        "<th>id</th><th>protocol</th><th>baseline</th><th>arms</th>"
        "<th>geomean vs baseline</th><th>skipped</th><th>cpu</th>"
        "</tr></thead>\n<tbody>\n"
    ]
    for r in runs:
        stale = ' <span class="stale">stale protocol</span>' if r["stale_protocol"] else ""
        href = f"{rel_prefix}run/{_esc(r['id'])}/index.html"
        geo = _fmt_geo(r["geomean_vs_baseline"])
        cpu = (r.get("machine") or {}).get("cpu_model") or ""
        parts.append(
            f'<tr><td><a href="{href}">{_esc(r["id"])}</a>{stale}</td>'
            f'<td>{_esc(r["protocol"])}</td>'
            f'<td>{_esc(r["baseline"])}</td>'
            f'<td>{_esc(", ".join(r["arms"]))}</td>'
            f'<td>{_esc(geo)}</td>'
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
        "memory_available", "cache_l1d", "topology", "affinity", "container",
    ]
    parts = ['<dl class="env">\n']
    seen = set()
    for k in keys:
        if k in env:
            parts.append(f"<dt>{_esc(k)}</dt><dd>{_esc(env[k])}</dd>\n")
            seen.add(k)
    for k, v in env.items():
        if k not in seen:
            parts.append(f"<dt>{_esc(k)}</dt><dd>{_esc(v)}</dd>\n")
    parts.append("</dl>\n")
    return "".join(parts)


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
        run_dir = Path(r["path"])
        run_out = out / "run" / r["id"]
        run_out.mkdir(parents=True, exist_ok=True)
        report_html = run_dir / "report.html"
        inner = ""
        if report_html.is_file():
            inner = _body_inner(report_html.read_text(encoding="utf-8"))
        elif (run_dir / "report.md").is_file():
            inner = "<pre>" + _esc((run_dir / "report.md").read_text(encoding="utf-8")) + "</pre>"
        banner = ""
        if r["fixture"]:
            banner = (
                '<div class="banner"><p><strong>Fixture / demo.</strong> '
                "Not a published score.</p></div>\n"
            )
        stale = ""
        if r["stale_protocol"]:
            stale = (
                f'<p class="stale">stale protocol {r["protocol"]} '
                f"(current is {CURRENT_PROTOCOL})</p>\n"
            )
        page = [
            '<nav><a href="../../index.html">all runs</a></nav>\n',
            banner,
            stale,
            f'<h1>{_esc(r["id"])}</h1>\n',
            "<h2>Machine</h2>\n",
            _env_dl(r.get("_env") or {}),
            inner,
        ]
        (run_out / "index.html").write_text(
            _page(r["id"], "".join(page)), encoding="utf-8"
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
