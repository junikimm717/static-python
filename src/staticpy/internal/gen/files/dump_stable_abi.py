"""Dump the CPython stable ABI manifest as JSON.

Run under the pyhost interpreter with the CPython srctree as argv[1]. It reuses
upstream's own parser so our idea of "in the ABI" cannot drift from theirs.
"""

import importlib.util
import json
import re
import sys
import types
from pathlib import Path


def load_upstream(srctree):
    # stable_abi.py imports these at module scope for actions we never call.
    # A minimal pyhost has no subprocess or csv; stubbing them keeps that
    # interpreter minimal, and parse_manifest touching one would still raise.
    for name in ("csv", "difflib", "pprint", "subprocess", "sysconfig"):
        if name in sys.modules:
            continue
        try:
            __import__(name)
        except ImportError:
            sys.modules[name] = types.ModuleType(name)

    path = srctree / "Tools" / "build" / "stable_abi.py"
    spec = importlib.util.spec_from_file_location("staticpy_stable_abi", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def header_words(srctree):
    """Every identifier in the headers Python.h reaches.

    Include/internal is deliberately absent: a name declared only there is still
    undeclared for us, and so still needs a synthetic extern.
    """
    root = srctree / "Include"
    include = re.compile(r'^\s*#\s*include\s+"([^"]+)"', re.M)
    seen, queue = set(), ["Python.h"]
    words = set()
    ident = re.compile(r"[A-Za-z_][A-Za-z0-9_]*")
    while queue:
        name = queue.pop()
        if name in seen:
            continue
        seen.add(name)
        header = root / name
        if not header.is_file():
            continue
        text = header.read_text(encoding="utf-8", errors="replace")
        words.update(ident.findall(text))
        queue.extend(include.findall(text))
    return words


def main():
    srctree = Path(sys.argv[1])
    upstream = load_upstream(srctree)
    declared = header_words(srctree)
    with open(srctree / "Misc" / "stable_abi.toml", "rb") as f:
        manifest = upstream.parse_manifest(f)

    items = [
        {
            "name": item.name,
            "kind": item.kind,
            "abi_only": bool(item.abi_only),
            "ifdef": item.ifdef,
            "declared": item.name in declared,
        }
        # ifdef=None means "do not filter": every feature macro survives into
        # the output and becomes a #ifdef in the generated C.
        for item in manifest.select({"function", "data"}, include_abi_only=True)
    ]
    json.dump({"items": items}, sys.stdout, indent=1, sort_keys=True)
    sys.stdout.write("\n")


main()
