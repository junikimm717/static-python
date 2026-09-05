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


def header_scan(srctree):
    """Identifiers, #define names, and PyAPI_FUNC names Python.h reaches.

    Include/internal is deliberately absent: a name declared only there is still
    undeclared for us, and so still needs a synthetic extern.

    A stable-ABI function the public header also #defines (Py_PACK_FULL_VERSION
    in 3.14) cannot have its address taken until that macro is #undef'd.
    """
    root = srctree / "Include"
    include = re.compile(r'^\s*#\s*include\s+"([^"]+)"', re.M)
    define = re.compile(r"^\s*#\s*define\s+([A-Za-z_][A-Za-z0-9_]*)", re.M)
    api_func = re.compile(r"PyAPI_FUNC\s*\([^)]+\)\s*([A-Za-z_][A-Za-z0-9_]*)\s*\(")
    seen, queue = set(), ["Python.h"]
    words, macros, api_funcs = set(), set(), set()
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
        macros.update(define.findall(text))
        api_funcs.update(api_func.findall(text))
        queue.extend(include.findall(text))
    return words, macros, api_funcs


def main():
    srctree = Path(sys.argv[1])
    upstream = load_upstream(srctree)
    declared, macros, api_funcs = header_scan(srctree)
    with open(srctree / "Misc" / "stable_abi.toml", "rb") as f:
        manifest = upstream.parse_manifest(f)

    items = [
        {
            "name": item.name,
            "kind": item.kind,
            "abi_only": bool(item.abi_only),
            "ifdef": item.ifdef,
            "declared": item.name in declared,
            # Header #define hiding a real PyAPI_FUNC. Taking &name fails
            # until the generator #undef's it; skip names that are macros
            # with no function behind them.
            "macro_hides_func": item.name in macros and item.name in api_funcs,
        }
        # ifdef=None means "do not filter": every feature macro survives into
        # the output and becomes a #ifdef in the generated C.
        for item in manifest.select({"function", "data"}, include_abi_only=True)
        if not (item.name in macros and item.name not in api_funcs and item.kind == "function")
    ]
    json.dump({"items": items}, sys.stdout, indent=1, sort_keys=True)
    sys.stdout.write("\n")


main()
