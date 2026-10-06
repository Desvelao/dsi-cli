"""Regression tests for the small CLI problems found while writing the testing guide."""

import os
import tempfile
import unittest
from pathlib import Path

from typer.testing import CliRunner

from src.dsipy.cli.app import main_app
from src.dsipy.crypto.keys import generate_keypair

runner = CliRunner()
WIDE = {"COLUMNS": "200"}

FULL = [
    "--title", "T", "--link", "https://example.com/feed.rss",
    "--description", "D", "--author", "A", "--email", "a@example.com",
]  # fmt: skip


class InTmpDir(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = Path(tmp.name)
        cwd = os.getcwd()
        os.chdir(self.dir)
        self.addCleanup(os.chdir, cwd)


class TestBuildMissingDirectory(InTmpDir):
    def test_missing_directory_is_an_error(self):
        result = runner.invoke(
            main_app, ["feeds", "build", "nope", "-o", "out.rss", *FULL]
        )
        self.assertEqual(result.exit_code, 1, result.output)
        self.assertIn("Directory not found", result.output)
        self.assertFalse(Path("out.rss").exists())

    def test_empty_existing_directory_still_builds_an_empty_feed(self):
        Path("empty").mkdir()
        result = runner.invoke(
            main_app, ["feeds", "build", "empty", "-o", "out.rss", *FULL]
        )
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertNotIn("<item>", Path("out.rss").read_text())


class TestFeedsAddFilename(InTmpDir):
    def test_interactive_default_is_a_markdown_file_in_feeds(self):
        result = runner.invoke(main_app, ["feeds", "add", "-i"], input="\n\n\n")
        self.assertEqual(result.exit_code, 0, result.output)
        created = list(Path("feeds").glob("*.md"))
        self.assertEqual(len(created), 1, result.output)
        self.assertEqual(
            list(Path(".").glob("*t*z")), []
        )  # no stray extensionless file

    def test_missing_parent_folder_is_created(self):
        result = runner.invoke(
            main_app,
            ["feeds", "add", "-t", "T", "-m", "M", "-f", "new/dir/post.md"],
        )
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertTrue(Path("new/dir/post.md").is_file())

    def test_non_markdown_name_warns(self):
        result = runner.invoke(
            main_app, ["feeds", "add", "-t", "T", "-m", "M", "-f", "notes"]
        )
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("does not end in .md", result.output)
        ok = runner.invoke(
            main_app, ["feeds", "add", "-t", "T", "-m", "M", "-f", "good.md"]
        )
        self.assertNotIn("does not end in .md", ok.output)


class TestEndorseMissingInput(InTmpDir):
    def make_card(self, name):
        priv = Path(f"{name}.key")
        result = runner.invoke(
            main_app,
            ["vcard", "create", "-o", f"{name}.vcf", "--fn", name,
             "--source", f"https://{name}.example/dsi.vcf", "--generate-key"],
        )  # fmt: skip
        self.assertEqual(result.exit_code, 0, result.output)
        os.replace("vcard_private.pem", priv)
        os.replace("vcard_public.pem", f"{name}.pub")
        return priv

    def test_only_missing_inputs_fail(self):
        priv = self.make_card("alice")
        result = runner.invoke(
            main_app, ["vcard", "endorse", "missing.vcf", "--priv", str(priv)]
        )
        self.assertEqual(result.exit_code, 1, result.output)
        self.assertIn("No valid vCard files found", result.output)

    def test_one_valid_input_among_missing_ones_still_works(self):
        priv = self.make_card("alice")
        self.make_card("bob")
        result = runner.invoke(
            main_app,
            ["vcard", "endorse", "bob.vcf", "missing.vcf", "--priv", str(priv)],
        )
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("X-ENDORSE", result.output)


class TestSigningKeyNotFound(InTmpDir):
    def build(self, priv, pub):
        Path("feeds").mkdir(exist_ok=True)
        Path("feeds/a.md").write_text("---\ntitle: A\ndate: 2024-01-01\n---\nBody\n")
        return runner.invoke(
            main_app,
            ["feeds", "build", "feeds", "-o", "out.rss", *FULL,
             "--sign-priv", str(priv), "--sign-pub", str(pub)],
        )  # fmt: skip

    def test_missing_private_key_file(self):
        _, pub, _ = generate_keypair()
        Path("pub.pem").write_bytes(pub)
        result = self.build("nope.pem", "pub.pem")
        self.assertEqual(result.exit_code, 1, result.output)
        self.assertIn("not found for --sign-priv", result.output)
        self.assertNotIn("MalformedFraming", result.output)
        self.assertFalse(Path("out.rss").exists())

    def test_missing_public_key_file(self):
        priv, _, _ = generate_keypair()
        Path("priv.pem").write_bytes(priv)
        result = self.build("priv.pem", "nope.pem")
        self.assertEqual(result.exit_code, 1, result.output)
        self.assertIn("not found for --sign-pub", result.output)

    def test_inline_pem_text_still_works(self):
        priv, pub, _ = generate_keypair()
        result = self.build(priv.decode(), pub.decode())
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("<signature", Path("out.rss").read_text())


class TestFetchDoneOnlyOnSuccess(InTmpDir):
    def test_failed_fetch_does_not_say_done(self):
        result = runner.invoke(
            main_app, ["vcard", "fetch", "http://example.com/dsi.vcf", "--dry-run"]
        )
        self.assertEqual(result.exit_code, 1, result.output)
        self.assertNotIn("Done.", result.output)
        self.assertIn("1 item(s) failed", result.output)

    def test_successful_run_says_done(self):
        Path("nosource.vcf").write_text(
            "BEGIN:VCARD\nVERSION:4.0\nFN:No Source\nEND:VCARD\n"
        )
        result = runner.invoke(main_app, ["vcard", "fetch", "nosource.vcf", "-n"])
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("Done.", result.output)


class TestCreateWithoutSource(InTmpDir):
    def test_warns_when_source_is_missing(self):
        result = runner.invoke(
            main_app, ["vcard", "create", "-o", "c.vcf", "--fn", "No Source"]
        )
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("No SOURCE", result.output)
        self.assertTrue(Path("c.vcf").is_file())

    def test_no_warning_with_source(self):
        result = runner.invoke(
            main_app,
            ["vcard", "create", "-o", "c.vcf", "--fn", "X",
             "--source", "https://x.example/dsi.vcf"],
        )  # fmt: skip
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertNotIn("No SOURCE", result.output)


class TestHelpTexts(unittest.TestCase):
    def help(self, *args):
        result = runner.invoke(main_app, [*args, "--help"], env=WIDE)
        self.assertEqual(result.exit_code, 0, result.output)
        return result.output

    def test_feeds_build_has_a_description(self):
        self.assertIn(
            "Build an RSS feed from a folder of Markdown posts", self.help("feeds")
        )

    def test_vcard_parse_says_json(self):
        out = self.help("vcard", "parse")
        self.assertIn("JSON", out)
        self.assertNotIn("human-readable", out)

    def test_publish_dry_run_is_explained(self):
        out = self.help("feeds", "publish")
        self.assertIn("still needs working provider credentials", out)
        self.assertIn("Show a diff", out)
