import tempfile
import unittest
from datetime import timezone
from pathlib import Path
from unittest.mock import patch

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import ed25519
from typer.testing import CliRunner

from src.dsipy.cli.app import main_app
from src.dsipy.core.identity import VCard
from src.dsipy.crypto.keys import public_key_to_b64der
from src.dsipy.crypto.verification import verify_endorsement_signature

runner = CliRunner()


def write_key(directory: Path, name: str, seed: bytes):
    key = ed25519.Ed25519PrivateKey.from_private_bytes(seed.ljust(32, b"\0"))
    path = directory / f"{name}.pem"
    path.write_bytes(
        key.private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.PKCS8,
            serialization.NoEncryption(),
        )
    )
    return key, path


def vcard_text(name, source, key_b64=None, extra=""):
    key_line = (
        f"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:{key_b64}\n" if key_b64 else ""
    )
    return (
        f"BEGIN:VCARD\nVERSION:4.0\nFN:{name}\nSOURCE:{source}\n"
        f"{key_line}{extra}END:VCARD\n"
    )


class TestEndorseCommand(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = Path(tmp.name)
        self.alice, self.alice_priv = write_key(self.dir, "alice", b"alice")
        self.bob, _ = write_key(self.dir, "bob", b"bob")
        self.bob_b64 = public_key_to_b64der(self.bob.public_key())
        self.bob_card = self.dir / "bob.vcf"
        self.bob_card.write_text(
            vcard_text("Bob", "https://bob.example/bob.vcf", self.bob_b64)
        )
        self.alice_card = self.dir / "alice.vcf"
        self.alice_card.write_text(
            vcard_text(
                "Alice",
                "https://alice.example/alice.vcf",
                public_key_to_b64der(self.alice.public_key()),
            )
        )

    def run_endorse(self, *args):
        return runner.invoke(
            main_app,
            ["vcard", "endorse", *args, "--priv", str(self.alice_priv)],
        )

    def test_prints_a_valid_endorsement(self):
        result = self.run_endorse(str(self.bob_card))
        self.assertEqual(result.exit_code, 0, result.output)
        line = next(l for l in result.output.splitlines() if l.startswith("X-ENDORSE"))
        sig = line.split("SIG=")[1].split(";")[0]
        self.assertEqual(len(sig), 128)
        self.assertTrue(
            verify_endorsement_signature(self.alice.public_key(), self.bob_b64, sig)
        )
        self.assertTrue(line.endswith(f":{self.bob_b64}"))

    def test_date_is_utc(self):
        result = self.run_endorse(str(self.bob_card))
        line = next(l for l in result.output.splitlines() if l.startswith("X-ENDORSE"))
        date = line.split("DATE=")[1].split(";")[0]
        with patch("src.dsipy.cli.vcard.datetime") as fake:
            fake.now.return_value.strftime.return_value = "X"
            self.run_endorse(str(self.bob_card))
            self.assertIs(fake.now.call_args.args[0], timezone.utc)
        self.assertRegex(date, r"^\d{8}T\d{6}Z$")

    def test_write_requires_destination(self):
        result = self.run_endorse(str(self.bob_card), "--write")
        self.assertEqual(result.exit_code, 1)
        self.assertIn("--vcard", result.output)

    def test_write_adds_endorsement_to_the_destination_once(self):
        args = (str(self.bob_card), "--vcard", str(self.alice_card), "--write")
        first = self.run_endorse(*args)
        self.assertEqual(first.exit_code, 0, first.output)
        profile = VCard(path=self.alice_card).profile
        self.assertEqual(len(profile.endorsements), 1)
        self.assertEqual(profile.endorsements[0].endorsee_key_b64, self.bob_b64)

        second = self.run_endorse(*args)
        self.assertEqual(second.exit_code, 0, second.output)
        self.assertIn("already exists", second.output)
        self.assertEqual(len(VCard(path=self.alice_card).profile.endorsements), 1)
        # the endorsed card is not modified
        self.assertEqual(len(VCard(path=self.bob_card).profile.endorsements), 0)

    def test_exit_code_is_non_zero_when_an_input_fails(self):
        no_key = self.dir / "nokey.vcf"
        no_key.write_text(vcard_text("NoKey", "https://n.example/n.vcf"))
        result = self.run_endorse(str(no_key))
        self.assertEqual(result.exit_code, 1)

    def test_rejects_a_non_ed25519_endorsee_key(self):
        bad = self.dir / "bad.vcf"
        bad.write_text(vcard_text("Bad", "https://b.example/b.vcf", "bm90LWEta2V5"))
        result = self.run_endorse(str(bad))
        self.assertEqual(result.exit_code, 1)
        self.assertNotIn("X-ENDORSE", result.output)

    def test_invalid_private_key_exits_with_error(self):
        broken = self.dir / "broken.pem"
        broken.write_text("not a key")
        result = runner.invoke(
            main_app,
            ["vcard", "endorse", str(self.bob_card), "--priv", str(broken)],
        )
        self.assertEqual(result.exit_code, 1)


class TestExitCodes(unittest.TestCase):
    def test_unhandled_exception_in_a_command_exits_with_1(self):
        with patch(
            "src.dsipy.cli.key.action_generate_keypair", side_effect=OSError("disk")
        ):
            result = runner.invoke(main_app, ["key", "create"])
        self.assertEqual(result.exit_code, 1)

    def test_key_pub_encode_missing_file(self):
        result = runner.invoke(main_app, ["key", "pub-encode", "/nonexistent/key.pem"])
        self.assertNotEqual(result.exit_code, 0)

    def test_key_pub_decode_without_content(self):
        result = runner.invoke(main_app, ["key", "pub-decode"], input="\n")
        self.assertEqual(result.exit_code, 1)

    def test_connections_without_vcards_exits_with_1(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = runner.invoke(main_app, ["connections", "feed", tmp])
        self.assertEqual(result.exit_code, 1)

    def test_fetch_exits_with_1_when_a_source_cannot_be_fetched(self):
        with tempfile.TemporaryDirectory() as tmp:
            card = Path(tmp) / "a.vcf"
            card.write_text(vcard_text("A", "https://a.example/a.vcf"))
            with patch(
                "src.dsipy.core.resolver.fetch_text", side_effect=ValueError("boom")
            ):
                result = runner.invoke(main_app, ["vcard", "fetch", str(card)])
            self.assertEqual(result.exit_code, 1, result.output)
            self.assertIn("Failed: 1", result.output)

    def test_fetch_does_not_overwrite_the_file_on_source_mismatch(self):
        with tempfile.TemporaryDirectory() as tmp:
            card = Path(tmp) / "a.vcf"
            original = vcard_text("A", "https://a.example/a.vcf")
            card.write_text(original)

            class Response:
                text = vcard_text("Evil", "https://evil.example/e.vcf")
                url = "https://a.example/a.vcf"
                headers = {}

            with patch("src.dsipy.core.resolver.fetch_text", return_value=Response()):
                result = runner.invoke(main_app, ["vcard", "fetch", str(card)])
            self.assertEqual(result.exit_code, 1, result.output)
            self.assertEqual(card.read_text(), original)


if __name__ == "__main__":
    unittest.main()
