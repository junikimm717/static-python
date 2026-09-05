"""Tests for manage_benchmarks.py. They copy the synthetic session under
tests/fixtures/ into a temp tree so they never mutate benchmarks/.
"""

from __future__ import annotations

import json
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

import manage_benchmarks as mb  # noqa: E402

FIXTURE_ID = "20000101T000000Z-x86_64"
FIXTURE = ROOT / "tests" / "fixtures" / FIXTURE_ID
REAL_ID = "20260904T143118Z-amd64"


def _copy_fixture(dest: Path) -> Path:
    shutil.copytree(FIXTURE, dest)
    return dest


class ImportTests(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.root = self.tmp / "repo"
        self.root.mkdir()
        self.session = _copy_fixture(self.tmp / "session" / FIXTURE_ID)
        (self.session / "venv").mkdir()
        (self.session / "venv" / "junk").write_text("no", encoding="utf-8")
        (self.session / "raw").mkdir()
        (self.session / "raw" / "x.json").write_text("{}", encoding="utf-8")
        (self.session / "logs").mkdir()
        (self.session / "logs" / "run.log").write_text("no", encoding="utf-8")
        self.addCleanup(shutil.rmtree, self.tmp, True)

    def test_import_copies_allowed_files_not_venv(self):
        dest = mb.import_session(self.root, self.session)
        self.assertEqual(dest.name, FIXTURE_ID)
        for name in (
            "manifest.json",
            "env.json",
            "report.json",
            "report.md",
            "report.html",
            "skipped.json",
        ):
            self.assertTrue((dest / name).is_file(), name)
        self.assertFalse((dest / "venv").exists())
        self.assertFalse((dest / "raw").exists())
        self.assertFalse((dest / "logs").exists())
        index = json.loads((self.root / "benchmarks" / "index.json").read_text(encoding="utf-8"))
        ids = [r["id"] for r in index["runs"]]
        self.assertIn(FIXTURE_ID, ids)
        entry = next(r for r in index["runs"] if r["id"] == FIXTURE_ID)
        self.assertEqual(entry["protocol"], mb.CURRENT_PROTOCOL)
        self.assertEqual(entry["git_revision"], "0000000000000000000000000000000000000000")
        self.assertFalse(entry["stale_protocol"])
        self.assertEqual(entry["suite"]["name"], "pyperformance")

    def test_import_refuses_duplicate_without_force(self):
        mb.import_session(self.root, self.session)
        with self.assertRaises(mb.ManagerError) as ctx:
            mb.import_session(self.root, self.session)
        self.assertIn("--force", str(ctx.exception))
        dest = mb.import_session(self.root, self.session, force=True)
        self.assertTrue((dest / "manifest.json").is_file())

    def test_import_refuses_protocol_zero(self):
        manifest = json.loads((self.session / "manifest.json").read_text(encoding="utf-8"))
        manifest["protocol"] = 0
        (self.session / "manifest.json").write_text(
            json.dumps(manifest, indent=2) + "\n", encoding="utf-8"
        )
        with self.assertRaises(mb.ManagerError) as ctx:
            mb.import_session(self.root, self.session)
        self.assertIn("protocol", str(ctx.exception))
        dest = mb.import_session(self.root, self.session, allow_stale=True)
        self.assertTrue((dest / "manifest.json").is_file())

    def test_import_refuses_previous_protocol(self):
        manifest = json.loads((self.session / "manifest.json").read_text(encoding="utf-8"))
        self.assertEqual(manifest["protocol"], mb.CURRENT_PROTOCOL)
        manifest["protocol"] = mb.CURRENT_PROTOCOL - 1
        (self.session / "manifest.json").write_text(
            json.dumps(manifest, indent=2) + "\n", encoding="utf-8"
        )
        with self.assertRaises(mb.ManagerError) as ctx:
            mb.import_session(self.root, self.session)
        self.assertIn("protocol", str(ctx.exception))
        dest = mb.import_session(self.root, self.session, allow_stale=True)
        self.assertTrue((dest / "manifest.json").is_file())

    def test_import_refuses_missing_manifest(self):
        (self.session / "manifest.json").unlink()
        with self.assertRaises(mb.ManagerError) as ctx:
            mb.import_session(self.root, self.session)
        self.assertIn("manifest.json", str(ctx.exception))

    def test_import_accepts_micro_suite(self):
        manifest = json.loads((self.session / "manifest.json").read_text(encoding="utf-8"))
        manifest["suite"] = {"name": "micro"}
        (self.session / "manifest.json").write_text(
            json.dumps(manifest, indent=2) + "\n", encoding="utf-8"
        )
        report = json.loads((self.session / "report.json").read_text(encoding="utf-8"))
        report["suite"] = {"name": "micro"}
        (self.session / "report.json").write_text(
            json.dumps(report, indent=2) + "\n", encoding="utf-8"
        )
        dest = mb.import_session(self.root, self.session)
        run = mb.load_run(dest, fixture=False)
        self.assertEqual(mb._suite_label(run["suite"]), "micro")


class DeleteTests(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.root = self.tmp / "repo"
        self.root.mkdir()
        self.session = _copy_fixture(self.tmp / "session" / FIXTURE_ID)
        self.addCleanup(shutil.rmtree, self.tmp, True)

    def test_delete_refuses_fixtures_without_flag(self):
        fx = self.root / "benchmarks" / "fixtures" / FIXTURE_ID
        _copy_fixture(fx)
        with self.assertRaises(mb.ManagerError) as ctx:
            mb.delete_run(self.root, FIXTURE_ID, yes=True)
        self.assertIn("fixtures", str(ctx.exception))
        self.assertTrue(fx.is_dir())
        mb.delete_run(self.root, FIXTURE_ID, yes=True, fixtures=True)
        self.assertFalse(fx.exists())

    def test_delete_removes_dir_and_index_entry(self):
        dest = mb.import_session(self.root, self.session)
        self.assertTrue(dest.is_dir())
        mb.delete_run(self.root, FIXTURE_ID, yes=True)
        self.assertFalse(dest.exists())
        index = json.loads((self.root / "benchmarks" / "index.json").read_text(encoding="utf-8"))
        self.assertEqual(index["runs"], [])


class VerifyTests(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.root = self.tmp / "repo"
        fx = self.root / "benchmarks" / "fixtures" / FIXTURE_ID
        _copy_fixture(fx)
        self.addCleanup(shutil.rmtree, self.tmp, True)

    def test_verify_fails_on_truncated_report(self):
        report = self.root / "benchmarks" / "fixtures" / FIXTURE_ID / "report.json"
        report.write_text("{", encoding="utf-8")
        with self.assertRaises(mb.ManagerError):
            mb.verify_runs(self.root)

    def test_committed_fixture_matches_current_protocol(self):
        run = mb.load_run(FIXTURE, fixture=True)
        self.assertEqual(run["protocol"], mb.CURRENT_PROTOCOL)
        self.assertFalse(run["stale_protocol"])
        self.assertEqual(run["suite"]["name"], "pyperformance")
        n = mb.verify_runs(ROOT)
        self.assertGreaterEqual(n, 1)

    def test_protocol_matches_go(self):
        text = (ROOT / "src" / "staticpy" / "internal" / "bench" / "protocol.go").read_text(
            encoding="utf-8"
        )
        m = re.search(r"const Protocol = (\d+)", text)
        self.assertIsNotNone(m)
        self.assertEqual(int(m.group(1)), mb.CURRENT_PROTOCOL)


class SiteTests(unittest.TestCase):
    def test_site_leads_with_geomean(self):
        out = Path(tempfile.mkdtemp()) / "site"
        self.addCleanup(shutil.rmtree, out.parent, True)
        mb.write_site(ROOT, out)
        index = (out / "index.html").read_text(encoding="utf-8")
        self.assertIn(REAL_ID, index)
        self.assertNotIn("Fixture / demo", index)
        self.assertIn(">pyperformance<", index)
        self.assertTrue((out / "run" / REAL_ID / "index.html").is_file())
        self.assertTrue((out / "data" / "index.json").is_file())
        page = (out / "run" / REAL_ID / "index.html").read_text(encoding="utf-8")
        self.assertIn("<h1>pyperformance comparison</h1>", page)
        self.assertLess(page.find("<h1>"), page.find("Geomean vs"))
        self.assertLess(page.find("Geomean vs"), page.find("Per-benchmark ratio"))
        self.assertLess(page.find("Per-benchmark ratio"), page.find("Interpreters"))
        self.assertLess(page.find("Interpreters"), page.find("Dependencies"))
        self.assertIn("<summary>Interpreters</summary>", page)
        self.assertIn("<summary>Dependencies</summary>", page)
        self.assertIn("<summary>Machine</summary>", page)
        self.assertIn("fingerprint", page)
        self.assertIn("spectre_v2", page)
        self.assertIn("telemetry", page)
        self.assertIn("git_revision", page)
        self.assertIn("binary_sha256", page)
        self.assertIn("whole-graph", page)
        self.assertIn("mimalloc", page)
        self.assertIn("pyperformance==1.14.0", page)
        self.assertNotIn("<th>packages</th>", page)
        self.assertIn('class="ratio win1"', page)
        self.assertIn('class="ratio win3"', page)
        self.assertIn('class="ratio lose3"', page)
        self.assertIn("magenta = outlier", page)
        self.assertIn('class="swatch ratio lose3"', page)
        self.assertIn('class="swatch ratio win3"', page)
        self.assertIn("data-tip=", page)
        self.assertIn("whole-program LTO", page)
        self.assertIn("Baseline for the ratios", page)
        self.assertIn("Hover an arm name", page)

    def test_site_badges_fixtures(self):
        tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, tmp, True)
        root = tmp / "repo"
        fx = root / "benchmarks" / "fixtures" / FIXTURE_ID
        _copy_fixture(fx)
        out = tmp / "site"
        mb.write_site(root, out)
        index = (out / "index.html").read_text(encoding="utf-8")
        self.assertIn(FIXTURE_ID, index)
        self.assertIn("Fixture / demo", index)
        page = (out / "run" / FIXTURE_ID / "index.html").read_text(encoding="utf-8")
        self.assertIn("Fixture / demo", page)
        self.assertIn("<h1>pyperformance comparison</h1>", page)


class InterpTipTests(unittest.TestCase):
    def test_tip_is_from_factors_not_the_label(self):
        tip = mb._interp_tip(
            {
                "label": "some-future-arm",
                "artifact_key": "abcd1234eeee",
                "factors": {
                    "linkage": "static",
                    "libc": "musl",
                    "lto": "per-dep",
                    "allocator": "jemalloc",
                    "pgo": False,
                },
            }
        )
        self.assertIn("Static musl interpreter", tip)
        self.assertIn("per-library LTO", tip)
        self.assertIn("jemalloc malloc", tip)
        self.assertIn("no PGO", tip)
        self.assertIn("artifact abcd1234eeee", tip)
        self.assertNotIn("some-future-arm", tip)

    def test_baseline_flag_uses_session_label(self):
        tip = mb._interp_tip(
            {"label": "whatever", "factors": {"linkage": "dynamic", "pgo": True}},
            baseline="whatever",
        )
        self.assertIn("Baseline for the ratios", tip)


class RatioTintTests(unittest.TestCase):
    def test_bands(self):
        self.assertEqual(mb._ratio_band(1.0), "")
        self.assertEqual(mb._ratio_band(1.02), "")
        self.assertEqual(mb._ratio_band(0.98), "")
        self.assertEqual(mb._ratio_band(0.5), "lose3")
        self.assertEqual(mb._ratio_band(0.8), "lose2")
        self.assertEqual(mb._ratio_band(0.95), "lose1")
        self.assertEqual(mb._ratio_band(1.10), "win1")
        self.assertEqual(mb._ratio_band(1.35), "win2")
        self.assertEqual(mb._ratio_band(2.0), "win3")
        self.assertEqual(mb._ratio_band(3.27), "win3")


class GitignoreTests(unittest.TestCase):
    def test_gitignore_unignores_allowed_files(self):
        text = (ROOT / "benchmarks" / ".gitignore").read_text(encoding="utf-8")
        for name in mb.ALLOWED_FILES:
            self.assertRegex(
                text,
                rf"(?m)^!\*\*/{re.escape(name)}$",
                msg=f"benchmarks/.gitignore must un-ignore {name}",
            )

    def test_dump_junk_is_ignored_session_files_are_not(self):
        probe = ROOT / "benchmarks" / "_probe_gitignore"
        self.addCleanup(shutil.rmtree, probe, True)
        if probe.exists():
            shutil.rmtree(probe)
        probe.mkdir()
        (probe / "manifest.json").write_text("{}\n", encoding="utf-8")
        (probe / "quiet.jsonl").write_text("x\n", encoding="utf-8")
        (probe / "venv").mkdir()
        (probe / "venv" / "junk").write_text("x\n", encoding="utf-8")
        (probe / "raw").mkdir()
        (probe / "raw" / "x.json").write_text("{}\n", encoding="utf-8")
        (probe / "logs").mkdir()
        (probe / "logs" / "run.log").write_text("x\n", encoding="utf-8")

        def ignored(rel: str) -> bool:
            r = subprocess.run(
                ["git", "check-ignore", "--no-index", "-q", rel],
                cwd=str(ROOT),
                check=False,
            )
            self.assertIn(r.returncode, (0, 1), rel)
            return r.returncode == 0

        prefix = "benchmarks/_probe_gitignore"
        self.assertFalse(ignored(f"{prefix}/manifest.json"))
        self.assertTrue(ignored(f"{prefix}/quiet.jsonl"))
        self.assertTrue(ignored(f"{prefix}/venv/junk"))
        self.assertTrue(ignored(f"{prefix}/raw/x.json"))
        self.assertTrue(ignored(f"{prefix}/logs/run.log"))
        real = f"benchmarks/{REAL_ID}"
        for name in mb.ALLOWED_FILES:
            self.assertFalse(ignored(f"{real}/{name}"), name)


class HelpTests(unittest.TestCase):
    def test_help_exits_zero(self):
        r = subprocess.run(
            [sys.executable, str(ROOT / "manage_benchmarks.py"), "--help"],
            cwd=str(ROOT),
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("import", r.stdout)


if __name__ == "__main__":
    unittest.main()
