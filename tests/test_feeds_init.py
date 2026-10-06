import os
import tempfile
import unittest
from pathlib import Path

from typer.testing import CliRunner

from src.dsipy.cli.app import main_app

runner = CliRunner()


class TestFeedsInit(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = Path(tmp.name)

    def init(self, *args):
        return runner.invoke(main_app, ["feeds", "init", *args])

    def test_creates_directory_with_sample_post(self):
        target = self.dir / "feeds"
        result = self.init(str(target))
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual([p.name for p in target.iterdir()], ["hello.md"])
        self.assertIn("title: Hello DSI", (target / "hello.md").read_text())

    def test_default_directory_is_feeds_in_the_current_directory(self):
        old = os.getcwd()
        os.chdir(self.dir)
        self.addCleanup(os.chdir, old)
        result = self.init()
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertTrue((self.dir / "feeds" / "hello.md").is_file())

    def test_nested_path(self):
        result = self.init(str(self.dir / "a" / "b" / "feeds"))
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertTrue((self.dir / "a" / "b" / "feeds" / "hello.md").is_file())

    def test_no_sample_creates_gitkeep(self):
        target = self.dir / "feeds"
        result = self.init(str(target), "--no-sample")
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual([p.name for p in target.iterdir()], [".gitkeep"])

    def test_no_sample_leaves_a_non_empty_directory_alone(self):
        target = self.dir / "feeds"
        target.mkdir()
        (target / "mine.md").write_text("x")
        self.init(str(target), "--no-sample")
        self.assertEqual([p.name for p in target.iterdir()], ["mine.md"])

    def test_running_twice_keeps_edited_files(self):
        target = self.dir / "feeds"
        self.init(str(target))
        (target / "hello.md").write_text("edited")
        result = self.init(str(target))
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual((target / "hello.md").read_text(), "edited")
        self.assertIn("already exists", result.output)

    def test_path_that_is_a_file_fails(self):
        file = self.dir / "feeds"
        file.write_text("x")
        result = self.init(str(file))
        self.assertEqual(result.exit_code, 1)
        self.assertEqual(file.read_text(), "x")

    def test_initialized_directory_builds(self):
        target = self.dir / "feeds"
        self.init(str(target))
        out = self.dir / "feeds.rss"
        result = runner.invoke(
            main_app,
            [
                "feeds",
                "build",
                str(target),
                "-o",
                str(out),
                "--title",
                "T",
                "--link",
                "https://a.example",
                "--description",
                "d",
                "--author",
                "A",
                "--email",
                "a@a.example",
            ],
        )
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(out.read_text().count("<item>"), 1)

    def test_unsupported_type_fails(self):
        result = self.init(str(self.dir / "feeds"), "--type", "nope")
        self.assertEqual(result.exit_code, 1)
        self.assertFalse((self.dir / "feeds").exists())


if __name__ == "__main__":
    unittest.main()


class TestFeedsAdd(unittest.TestCase):
    def test_add_creates_a_post(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "a.md"
            result = runner.invoke(
                main_app,
                [
                    "feeds",
                    "add",
                    "--title",
                    "T",
                    "--message",
                    "M",
                    "--filename",
                    str(path),
                ],
            )
            self.assertEqual(result.exit_code, 0, result.output)
            self.assertIn("title: T", path.read_text())

    def test_new_no_longer_exists(self):
        result = runner.invoke(main_app, ["feeds", "new"])
        self.assertEqual(result.exit_code, 2)
        self.assertIn("No such command", result.output)
