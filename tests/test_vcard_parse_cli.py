import json
import tempfile
import unittest
from pathlib import Path

from typer.testing import CliRunner

from src.dsipy.cli.app import main_app

runner = CliRunner()

CARD = "BEGIN:VCARD\nVERSION:4.0\nFN:Alice\nSOURCE:https://alice.example/dsi.vcf\nEND:VCARD\n"


class ParseCliTests(unittest.TestCase):
    def test_parse_file(self):
        with tempfile.TemporaryDirectory() as d:
            path = Path(d) / "a.vcf"
            path.write_text(CARD)
            result = runner.invoke(main_app, ["vcard", "parse", str(path)])
        self.assertEqual(result.exit_code, 0, result.output)
        json.loads(result.output)
        self.assertIn("Alice", result.output)

    def test_parse_stdin_without_argument(self):
        result = runner.invoke(main_app, ["vcard", "parse"], input=CARD)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("Alice", result.output)

    def test_parse_dash(self):
        result = runner.invoke(main_app, ["vcard", "parse", "-"], input=CARD)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("Alice", result.output)

    def test_no_input_single_message(self):
        result = runner.invoke(main_app, ["vcard", "parse"], input="")
        self.assertEqual(result.exit_code, 1)
        self.assertEqual(result.output.count("No input data provided"), 1)
        self.assertNotIn("Failed to parse", result.output)

    def test_empty_dash_single_message(self):
        result = runner.invoke(main_app, ["vcard", "parse", "-"], input="")
        self.assertEqual(result.exit_code, 1)
        self.assertEqual(result.output.count("No input data provided"), 1)
        self.assertNotIn("Failed to parse", result.output)

    def test_nonexistent_file(self):
        result = runner.invoke(main_app, ["vcard", "parse", "/nonexistent/x.vcf"])
        self.assertEqual(result.exit_code, 1)
        self.assertEqual(result.output.count("Failed to parse vCard"), 1)


if __name__ == "__main__":
    unittest.main()
