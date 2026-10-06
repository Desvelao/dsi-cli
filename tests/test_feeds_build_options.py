import tempfile
import unittest
from pathlib import Path

from typer.testing import CliRunner

from src.dsipy.cli.app import main_app
from src.dsipy.crypto.keys import generate_keypair, load_public_key_b64_der
from src.dsipy.feeds.signing import INVALID, VALID, verify_feed_items

runner = CliRunner()

FULL = [
    "--title", "T", "--link", "https://example.com/feed.rss",
    "--description", "D", "--author", "A", "--email", "a@example.com",
]  # fmt: skip


class BuildBase(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = Path(tmp.name)
        self.src = self.dir / "feeds"
        self.src.mkdir()
        self.out = self.dir / "out.rss"
        for n, day in (("a", 1), ("b", 2), ("c", 3)):
            self.post(n, f"Post {n}", f"2024-01-0{day}T00:00:00Z", f"Body {n}")

    def post(self, name, title, date, body, extra=""):
        (self.src / f"{name}.md").write_text(
            f"---\ntitle: {title}\ndate: {date}\n{extra}---\n{body}\n"
        )

    def build(self, *extra):
        return runner.invoke(
            main_app,
            ["feeds", "build", str(self.src), "-o", str(self.out), *FULL, *extra],
        )

    def xml(self):
        return self.out.read_text()


class TestBuildLimit(BuildBase):
    def test_limit_keeps_newest_items(self):
        result = self.build("--limit", "2")
        self.assertEqual(result.exit_code, 0, result.output)
        xml = self.xml()
        self.assertEqual(xml.count("<item>"), 2)
        self.assertIn("Post c", xml)
        self.assertIn("Post b", xml)
        self.assertNotIn("Post a", xml)

    def test_limit_zero_gives_an_empty_feed(self):
        result = self.build("--limit", "0")
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(self.xml().count("<item>"), 0)

    def test_negative_limit_is_rejected(self):
        result = self.build("--limit", "-1")
        self.assertEqual(result.exit_code, 2, result.output)
        self.assertFalse(self.out.exists())

    def test_no_limit_includes_everything(self):
        self.build()
        self.assertEqual(self.xml().count("<item>"), 3)


class TestBuildVars(BuildBase):
    def test_var_and_var_file_with_precedence(self):
        self.post("v", "Hi {{ who }}", "2024-02-01T00:00:00Z", "{{ site }} {{ who }}")
        varfile = self.dir / "vars.env"
        varfile.write_text("# comment\n\nsite = https://file.example\nwho=file\n")
        result = self.build(
            "--var-file", str(varfile), "--var", "who=cli", "--var", "x=a=b"
        )
        self.assertEqual(result.exit_code, 0, result.output)
        xml = self.xml()
        self.assertIn("Hi cli", xml)
        self.assertIn("https://file.example cli", xml)
        self.assertNotIn("{{ site }}", xml)

    def test_var_without_equals_is_a_clean_error(self):
        result = self.build("--var", "foo")
        self.assertNotEqual(result.exit_code, 0)
        self.assertIsInstance(result.exception, SystemExit)
        self.assertIn("key=value", result.output)
        self.assertFalse(self.out.exists())

    def test_missing_var_file_exits_1(self):
        result = self.build("--var-file", str(self.dir / "nope.env"))
        self.assertEqual(result.exit_code, 1)
        self.assertIn("Variable file not found", result.output)


class TestBuildHtml(BuildBase):
    def test_html_content_is_wrapped_in_cdata(self):
        self.post("h", "Rich", "2024-03-01T00:00:00Z", "Some **bold**",
                  extra="use_html_content: true\n")  # fmt: skip
        result = self.build()
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("<![CDATA[<p>Some <strong>bold</strong></p>]]>", self.xml())


class TestBuildSigning(BuildBase):
    def keys(self):
        priv, pub, b64 = generate_keypair()
        p, q = self.dir / "k.pem", self.dir / "k.pub.pem"
        p.write_bytes(priv)
        q.write_bytes(pub)
        return p, q, b64

    def test_signed_feed_round_trips_through_verify(self):
        priv, pub, b64 = self.keys()
        result = self.build("--sign-priv", str(priv), "--sign-pub", str(pub))
        self.assertEqual(result.exit_code, 0, result.output)
        results = verify_feed_items(self.xml(), {b64: load_public_key_b64_der(b64)})
        self.assertEqual([r.status for r in results], [VALID] * 3)
        ok = runner.invoke(
            main_app, ["feeds", "verify", str(self.out), "--pub", str(pub)]
        )
        self.assertEqual(ok.exit_code, 0, ok.output)

    def test_tampered_signed_feed_fails_verify(self):
        priv, pub, _ = self.keys()
        self.build("--sign-priv", str(priv), "--sign-pub", str(pub))
        self.out.write_text(self.xml().replace("Body a", "Body X"))
        bad = runner.invoke(
            main_app, ["feeds", "verify", str(self.out), "--pub", str(pub)]
        )
        self.assertEqual(bad.exit_code, 1, bad.output)
        self.assertIn("invalid", bad.output)

    def test_only_one_key_is_an_error(self):
        priv, pub, _ = self.keys()
        for opt, path in (("--sign-priv", priv), ("--sign-pub", pub)):
            self.out.unlink(missing_ok=True)
            result = self.build(opt, str(path))
            self.assertEqual(result.exit_code, 1, result.output)
            self.assertIn("both --sign-priv and --sign-pub", result.output)
            self.assertFalse(self.out.exists())

    def test_unsigned_build_has_no_signature(self):
        self.build()
        self.assertNotIn("<signature", self.xml())


class TestConnectionsFeedOutput(unittest.TestCase):
    def test_writes_opml_file_and_warns_about_malformed_cards(self):
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            (d / "good.vcf").write_text(
                "BEGIN:VCARD\nVERSION:4.0\nFN:Good\nX-FEED:https://e.com/good.xml\nEND:VCARD\n"
            )
            (d / "bad.vcf").write_text(
                "BEGIN:VCARD\nVERSION:4.0\nFN:Bad\nnot a valid line\n"
                "X-FEED:https://e.com/bad.xml\nEND:VCARD\n"
            )
            out = d / "sub" / "out.opml"
            result = runner.invoke(
                main_app, ["connections", "feed", str(d), "-o", str(out)]
            )
            self.assertEqual(result.exit_code, 0, result.output)
            text = out.read_text()
            self.assertIn("https://e.com/good.xml", text)
            self.assertNotIn("bad.xml", text)
            self.assertIn("OPML file generated", result.output)


if __name__ == "__main__":
    unittest.main()
