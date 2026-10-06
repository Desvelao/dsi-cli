import tempfile
import unittest
from pathlib import Path

from typer.testing import CliRunner

from src.dsipy.cli.app import main_app

runner = CliRunner()

FULL = {
    "--title": "T",
    "--link": "https://example.com/feed.rss",
    "--description": "D",
    "--author": "A",
    "--email": "a@example.com",
}


class TestFeedsBuild(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = Path(tmp.name)
        self.src = self.dir / "feeds"
        self.src.mkdir()
        (self.src / "hello.md").write_text(
            "---\ntitle: Hello\ndate: 2024-01-01T00:00:00Z\n---\nBody\n"
        )
        self.out = self.dir / "out.rss"

    def build(self, opts, *extra, **kw):
        args = ["feeds", "build", str(self.src), "--output", str(self.out)]
        for k, v in opts.items():
            args += [k, v]
        return runner.invoke(main_app, args + list(extra), **kw)

    def test_non_interactive_missing_option_exits_1(self):
        for missing in FULL:
            opts = {k: v for k, v in FULL.items() if k != missing}
            result = self.build(opts)  # no input supplied: a prompt would abort
            self.assertEqual(result.exit_code, 1, (missing, result.output))
            self.assertIn(f"The {missing[2:]} cannot be empty", result.output)
            self.assertFalse(self.out.exists())

    def test_non_interactive_all_options_writes_feed(self):
        result = self.build(FULL)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("<title>T</title>", self.out.read_text())

    def test_interactive_prompts_for_missing_value(self):
        opts = {k: v for k, v in FULL.items() if k != "--title"}
        result = self.build(opts, "--interactive", input="Prompted Title\n")
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("Provide the title", result.output)
        self.assertIn("Prompted Title", self.out.read_text())
