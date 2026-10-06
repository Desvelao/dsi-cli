import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from typer.testing import CliRunner

from src.dsipy.cli.app import main_app

runner = CliRunner()

URL = "https://a.example/a.vcf"


def card(name, source=URL):
    return f"BEGIN:VCARD\nVERSION:4.0\nFN:{name}\nSOURCE:{source}\nEND:VCARD\n"


def response(text):
    class Response:
        url = URL
        headers = {}

    Response.text = text
    return Response()


class TestFetchWriteBehaviour(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = Path(tmp.name)
        cwd = os.getcwd()
        os.chdir(self.dir)
        self.addCleanup(os.chdir, cwd)
        self.old = card("Old")
        self.new = card("New")

    def run_fetch(self, remote, *args):
        with patch(
            "src.dsipy.core.resolver.fetch_text", return_value=response(remote)
        ):
            return runner.invoke(main_app, ["vcard", "fetch", *args])

    def test_dry_run_url_new_destination_writes_nothing(self):
        result = self.run_fetch(self.new, "--dry-run", URL)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(list(self.dir.iterdir()), [])
        self.assertIn("Would download: 1", result.output)

    def test_dry_run_url_existing_destination_untouched(self):
        dest = self.dir / "a.vcf"
        dest.write_text(self.old)
        result = self.run_fetch(self.new, "--dry-run", "--backup", URL)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(dest.read_text(), self.old)
        self.assertEqual([p.name for p in self.dir.iterdir()], ["a.vcf"])
        self.assertIn("Would update: 1", result.output)

    def test_dry_run_file_input_untouched(self):
        dest = self.dir / "b.vcf"
        dest.write_text(self.old)
        result = self.run_fetch(self.new, "--dry-run", "--backup", str(dest))
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(dest.read_text(), self.old)
        self.assertEqual([p.name for p in self.dir.iterdir()], ["b.vcf"])
        self.assertIn("Would update: 1", result.output)
        self.assertNotIn("Updated", result.output)

    def test_unchanged_file_is_not_counted_or_backed_up(self):
        dest = self.dir / "b.vcf"
        dest.write_text(self.old)
        result = self.run_fetch(self.old, "--backup", str(dest))
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("Updated: 0", result.output)
        self.assertIn("Unchanged: 1", result.output)
        self.assertEqual([p.name for p in self.dir.iterdir()], ["b.vcf"])

    def test_unchanged_url_destination_is_not_counted(self):
        dest = self.dir / "a.vcf"
        dest.write_text(self.old)
        result = self.run_fetch(self.old, "--backup", URL)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("Updated: 0", result.output)
        self.assertIn("Downloaded: 0", result.output)
        self.assertIn("Unchanged: 1", result.output)
        self.assertEqual([p.name for p in self.dir.iterdir()], ["a.vcf"])

    def test_url_diff_for_existing_destination(self):
        (self.dir / "a.vcf").write_text(self.old)
        result = self.run_fetch(self.new, "--dry-run", "--diff", URL)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("-FN:Old", result.output)
        self.assertIn("+FN:New", result.output)

    def test_url_backup_and_update_existing_destination(self):
        dest = self.dir / "a.vcf"
        dest.write_text(self.old)
        result = self.run_fetch(self.new, "--backup", URL)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(dest.read_text(), self.new)
        self.assertEqual((self.dir / "a.vcf.bak").read_text(), self.old)
        self.assertIn("Updated: 1", result.output)

    def test_url_new_destination_is_downloaded(self):
        result = self.run_fetch(self.new, URL)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual((self.dir / "a.vcf").read_text(), self.new)
        self.assertIn("Downloaded: 1", result.output)


if __name__ == "__main__":
    unittest.main()
