"""Tests for manage_benchmarks.py. They copy the committed fixture into a
temp tree so they never mutate benchmarks/.
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
FIXTURE = ROOT / "benchmarks" / "fixtures" / FIXTURE_ID


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
    def test_site_contains_fixture_id_and_heading(self):
        out = Path(tempfile.mkdtemp()) / "site"
        self.addCleanup(shutil.rmtree, out.parent, True)
        mb.write_site(ROOT, out)
        index = (out / "index.html").read_text(encoding="utf-8")
        self.assertIn(FIXTURE_ID, index)
        self.assertIn("Fixture / demo", index)
        self.assertTrue((out / "run" / FIXTURE_ID / "index.html").is_file())
        self.assertTrue((out / "data" / "index.json").is_file())
        page = (out / "run" / FIXTURE_ID / "index.html").read_text(encoding="utf-8")
        self.assertIn("fingerprint", page)
        self.assertIn("spectre_v2", page)
        self.assertIn("telemetry", page)
        self.assertIn("git_revision", page)
        self.assertIn("binary_sha256", page)
        self.assertIn("whole-graph", page)
        self.assertIn("mimalloc", page)
        self.assertIn("pyperformance==1.14.0", page)


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
