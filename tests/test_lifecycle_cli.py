from datetime import datetime, timezone
import json
import tempfile
import unittest
from pathlib import Path

from typer.testing import CliRunner

from src.dsipy.cli.app import main_app
from src.dsipy.core.canonical import normalize_vcard
from src.dsipy.core.lifecycle import add_key, revoke_key, rotate_key
from src.dsipy.core.validator import validate_profile
from src.dsipy.vcard.parser import parse_vcard

from .helpers import crlf, dsi_vcard, make_key, private_pem

runner = CliRunner()
_, ALICE = make_key("alice")
_, NEW = make_key("new")
WHEN = datetime(2026, 3, 1, 12, 0, 0, tzinfo=timezone.utc)


class TestRotateAndRevoke(unittest.TestCase):
    def test_rotate_makes_the_new_key_preferred_and_revokes_the_old(self):
        updated = rotate_key(dsi_vcard(), NEW, when=WHEN)
        profile = parse_vcard(updated)
        self.assertEqual(
            {k.key_b64: k.pref for k in profile.keys}, {ALICE: None, NEW: 1}
        )
        [revocation] = profile.revocations
        self.assertEqual(
            (revocation.key_b64, revocation.reason, revocation.date),
            (ALICE, "rotated", "20260301T120000Z"),
        )
        self.assertTrue(validate_profile(profile).valid)
        self.assertTrue(updated.endswith("END:VCARD\r\n"))
        self.assertNotIn("\n", updated.replace("\r\n", ""))

    def test_rotate_keeps_other_properties(self):
        text = dsi_vcard(
            "X-ACME-UNKNOWN;FOO=bar:keep me", "X-SOCIAL;PLATFORM=github:alice"
        )
        updated = rotate_key(text, NEW, when=WHEN)
        self.assertIn("X-ACME-UNKNOWN;FOO=bar:keep me", updated)
        self.assertIn("X-SOCIAL;PLATFORM=github:alice", updated)
        self.assertIn("SOURCE:https://alice.example/dsi.vcf", updated)

    def test_rotate_errors(self):
        with self.assertRaises(ValueError):  # new key already present
            rotate_key(dsi_vcard(), ALICE)
        with self.assertRaises(ValueError):  # invalid key
            rotate_key(dsi_vcard(), "bm90LWEta2V5")
        with self.assertRaises(ValueError):  # no preferred key
            rotate_key(dsi_vcard(keys=False), NEW)
        with self.assertRaises(ValueError):  # bad reason
            rotate_key(dsi_vcard(), NEW, reason="compromised")

    def test_revoke_adds_revkey_and_clears_pref(self):
        updated = revoke_key(dsi_vcard(), ALICE, "compromised", WHEN)
        profile = parse_vcard(updated)
        self.assertEqual(profile.revocations[0].reason, "compromised")
        self.assertIsNone(profile.keys[0].pref)

    def test_revoke_records_the_alg_of_the_key_line(self):
        text = crlf(
            "BEGIN:VCARD",
            "VERSION:4.0",
            "FN:Alice Example",
            f"KEY;TYPE=public;ALG=ed448;PREF=1;ENCODING=b:{ALICE}",
            "END:VCARD",
        )
        updated = revoke_key(text, ALICE, "lost", WHEN)
        [revkey] = [l for l in updated.split("\r\n") if l.startswith("REVKEY")]
        self.assertIn("ALG=ed448", revkey)
        self.assertNotIn("ed25519", revkey)

    def test_shared_constants_have_a_single_home(self):
        from src.dsipy.core import lifecycle, model, validator
        from src.dsipy.endorsements import verify

        self.assertIs(validator.REVOCATION_REASONS, model.REVOCATION_REASONS)
        self.assertIs(lifecycle.REVOCATION_REASONS, model.REVOCATION_REASONS)
        self.assertEqual(verify.DSI_DATE_FORMAT, model.DSI_DATE_FORMAT)
        self.assertEqual(lifecycle.DATE_FORMAT, model.DSI_DATE_FORMAT)

    def test_revoke_errors(self):
        with self.assertRaises(ValueError):
            revoke_key(dsi_vcard(), NEW, "lost")  # not listed
        with self.assertRaises(ValueError):
            revoke_key(dsi_vcard(), ALICE, "whatever")
        with self.assertRaises(ValueError):
            revoke_key(revoke_key(dsi_vcard(), ALICE, "lost"), ALICE, "lost")


class TestNormalize(unittest.TestCase):
    MESSY = (
        "BEGIN:VCARD\n"
        "version:4.0\n"
        "x-social;platform=github:alice\n"
        f"KEY;ENCODING=b;PREF=1;ALG=ed25519;TYPE=public:{ALICE}\n"
        "SOURCE:https://alice.example/dsi.vcf\n"
        "X-ZZZ:last\n"
        "FN:Alice\n"
        "X-AAA:first unknown\n"
        "END:VCARD\n"
    )

    def test_deterministic_form(self):
        text = normalize_vcard(parse_vcard(self.MESSY))
        self.assertEqual(
            text,
            crlf(
                "BEGIN:VCARD",
                "VERSION:4.0",
                "FN:Alice",
                "SOURCE:https://alice.example/dsi.vcf",
                f"KEY;ALG=ed25519;ENCODING=b;PREF=1;TYPE=public:{ALICE}",
                "X-SOCIAL;PLATFORM=github:alice",
                "X-AAA:first unknown",
                "X-ZZZ:last",
                "END:VCARD",
            ),
        )

    def test_is_idempotent(self):
        once = normalize_vcard(parse_vcard(self.MESSY))
        self.assertEqual(normalize_vcard(parse_vcard(once)), once)

    def test_equivalent_inputs_give_the_same_output(self):
        folded = self.MESSY.replace("FN:Alice", "FN:Ali\r\n ce").replace("\n", "\r\n")
        folded = folded.replace("\r\r", "\r")
        self.assertEqual(
            normalize_vcard(parse_vcard(folded)),
            normalize_vcard(parse_vcard(self.MESSY)),
        )

    def test_malformed_vcard_is_refused(self):
        with self.assertRaises(ValueError):
            normalize_vcard(parse_vcard(dsi_vcard("garbage")))


class TestCommands(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = Path(tmp.name)
        self.card = self.dir / "alice.vcf"
        self.card.write_text(dsi_vcard(), encoding="utf-8", newline="")

    def test_validate_json_ok(self):
        result = runner.invoke(
            main_app, ["vcard", "validate", str(self.card), "--json"]
        )
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(
            json.loads(result.output), {"valid": True, "errors": [], "warnings": []}
        )

    def test_validate_reports_errors_with_exit_code_1(self):
        self.card.write_text(dsi_vcard().replace("SOURCE:", "X-NOSOURCE:"), newline="")
        result = runner.invoke(
            main_app, ["vcard", "validate", str(self.card), "--json"]
        )
        self.assertEqual(result.exit_code, 1)
        data = json.loads(result.output)
        self.assertFalse(data["valid"])
        self.assertEqual(data["errors"][0]["code"], "source-missing")

    def test_validate_strict_fails_on_warnings(self):
        self.card.write_text(dsi_vcard(keys=False), newline="")
        self.assertEqual(
            runner.invoke(main_app, ["vcard", "validate", str(self.card)]).exit_code, 0
        )
        self.assertEqual(
            runner.invoke(
                main_app, ["vcard", "validate", str(self.card), "--strict"]
            ).exit_code,
            1,
        )

    def test_validate_missing_file(self):
        result = runner.invoke(
            main_app, ["vcard", "validate", str(self.dir / "nope.vcf"), "--json"]
        )
        self.assertEqual(result.exit_code, 1)
        self.assertFalse(json.loads(result.output)["valid"])

    def test_validate_reads_stdin(self):
        result = runner.invoke(
            main_app, ["vcard", "validate", "-", "--json"], input=dsi_vcard()
        )
        self.assertEqual(result.exit_code, 0, result.output)

    def test_inspect_does_not_modify_the_file(self):
        before = self.card.read_bytes()
        result = runner.invoke(main_app, ["vcard", "inspect", str(self.card)])
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("Alice Example", result.output)
        self.assertIn("https://alice.example/dsi.vcf", result.output)
        self.assertEqual(self.card.read_bytes(), before)

    def test_normalize_prints_and_only_writes_on_request(self):
        before = self.card.read_bytes()
        result = runner.invoke(main_app, ["vcard", "normalize", str(self.card)])
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(self.card.read_bytes(), before)
        self.card.write_text(TestNormalize.MESSY, newline="")
        result = runner.invoke(
            main_app, ["vcard", "normalize", str(self.card), "--write"]
        )
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(
            self.card.read_bytes().decode(),
            normalize_vcard(parse_vcard(TestNormalize.MESSY)),
        )

    def test_verify_command(self):
        from .test_validator import ALICE_PRIV, BOB, endorse_line

        self.card.write_text(dsi_vcard(endorse_line(ALICE_PRIV, BOB)), newline="")
        ok = runner.invoke(main_app, ["vcard", "verify", str(self.card)])
        self.assertEqual(ok.exit_code, 0, ok.output)
        self.assertIn("valid", ok.output)
        self.card.write_text(
            dsi_vcard(f"X-ENDORSE;SIG={'0' * 128};ENCODING=b:{BOB}"), newline=""
        )
        self.assertEqual(
            runner.invoke(main_app, ["vcard", "verify", str(self.card)]).exit_code, 1
        )

    def test_key_rotate_command(self):
        priv, pub = self.dir / "new.pem", self.dir / "new_pub.pem"
        result = runner.invoke(
            main_app,
            ["key", "rotate", str(self.card), "--priv", str(priv), "--pub", str(pub)],
        )
        self.assertEqual(result.exit_code, 0, result.output)
        profile = parse_vcard(self.card.read_text())
        self.assertEqual(len(profile.keys), 2)
        self.assertEqual(profile.revocations[0].key_b64, ALICE)
        self.assertTrue(priv.exists() and pub.exists())
        self.assertTrue(validate_profile(profile).valid)
        # existing key files are never overwritten
        again = runner.invoke(
            main_app,
            ["key", "rotate", str(self.card), "--priv", str(priv), "--pub", str(pub)],
        )
        self.assertEqual(again.exit_code, 1)

    def test_key_revoke_command(self):
        result = runner.invoke(
            main_app,
            ["key", "revoke", str(self.card), "--key", ALICE, "--reason", "lost"],
        )
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(
            parse_vcard(self.card.read_text()).revocations[0].reason, "lost"
        )
        bad = runner.invoke(
            main_app,
            ["key", "revoke", str(self.card), "--key", NEW, "--reason", "lost"],
        )
        self.assertEqual(bad.exit_code, 1)

    def test_feeds_verify_command(self):
        from datetime import datetime as dt

        from src.dsipy.feeds.rss import RSSFeed

        priv, pub_b64 = make_key("alice")
        feed = self.dir / "feeds.rss"
        feed.write_text(
            RSSFeed.build(
                "T",
                "https://a.example",
                "d",
                "A",
                "a@a.example",
                "en",
                dt(2026, 1, 1),
                [{"id": "1", "title": "Hi", "date": dt(2026, 1, 1), "content": "body"}],
                sign={"key": priv, "id": pub_b64},
            )
        )
        ok = runner.invoke(
            main_app, ["feeds", "verify", str(feed), "--vcard", str(self.card)]
        )
        self.assertEqual(ok.exit_code, 0, ok.output)
        feed.write_text(feed.read_text().replace("body", "tampered"))
        bad = runner.invoke(
            main_app, ["feeds", "verify", str(feed), "--vcard", str(self.card)]
        )
        self.assertEqual(bad.exit_code, 1)
        none = runner.invoke(main_app, ["feeds", "verify", str(feed)])
        self.assertEqual(none.exit_code, 1)


if __name__ == "__main__":
    unittest.main()


class TestRotationIsValid(unittest.TestCase):
    def test_a_rotated_vcard_validates_without_warnings(self):
        updated = rotate_key(dsi_vcard(), NEW, when=WHEN)
        result = validate_profile(parse_vcard(updated))
        self.assertEqual((result.errors, result.warnings), ([], []))


class TestAddKey(unittest.TestCase):
    def test_add_when_there_is_no_key(self):
        updated = add_key(dsi_vcard(keys=False), NEW)
        profile = parse_vcard(updated)
        self.assertEqual([(k.key_b64, k.pref) for k in profile.keys], [(NEW, 1)])
        self.assertTrue(validate_profile(profile).valid)

    def test_add_when_every_key_is_revoked(self):
        revoked = revoke_key(dsi_vcard(), ALICE, "lost", WHEN)
        updated = add_key(revoked, NEW)
        profile = parse_vcard(updated)
        self.assertEqual(
            {k.key_b64: k.pref for k in profile.keys}, {ALICE: None, NEW: 1}
        )
        self.assertEqual(profile.revocations[0].key_b64, ALICE)
        result = validate_profile(profile)
        self.assertEqual((result.errors, result.warnings), ([], []))

    def test_add_demotes_the_previous_preferred_key(self):
        profile = parse_vcard(add_key(dsi_vcard(), NEW))
        self.assertEqual({k.key_b64: k.pref for k in profile.keys}[ALICE], None)
        self.assertEqual(profile.revocations, [])

    def test_no_pref_keeps_the_others(self):
        profile = parse_vcard(add_key(dsi_vcard(), NEW, preferred=False))
        self.assertEqual(
            {k.key_b64: k.pref for k in profile.keys}, {ALICE: 1, NEW: None}
        )

    def test_errors(self):
        with self.assertRaises(ValueError):
            add_key(dsi_vcard(), ALICE)  # already listed
        with self.assertRaises(ValueError):
            add_key(dsi_vcard(), "bm90LWEta2V5")  # not a key
        with self.assertRaises(ValueError):  # revoked keys cannot come back
            add_key(revoke_key(dsi_vcard(), ALICE, "lost"), ALICE)


class TestAddCommand(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = Path(tmp.name)
        self.card = self.dir / "alice.vcf"
        self.card.write_text(dsi_vcard(), encoding="utf-8", newline="")

    def run_cli(self, *args):
        return runner.invoke(main_app, list(args))

    def test_revoke_all_then_rotate_fails_and_add_works(self):
        revoke = self.run_cli(
            "key", "revoke", str(self.card), "--key", ALICE, "--reason", "lost"
        )
        self.assertEqual(revoke.exit_code, 0, revoke.output)
        self.assertIn("no usable key left", revoke.output)
        self.assertIn("key add", revoke.output)

        priv, pub = self.dir / "n.pem", self.dir / "n_pub.pem"
        rotate = self.run_cli(
            "key", "rotate", str(self.card), "--priv", str(priv), "--pub", str(pub)
        )
        self.assertEqual(rotate.exit_code, 1)
        self.assertIn("key add", rotate.output)
        self.assertFalse(priv.exists())

        add = self.run_cli(
            "key", "add", str(self.card), "--priv", str(priv), "--pub", str(pub)
        )
        self.assertEqual(add.exit_code, 0, add.output)
        profile = parse_vcard(self.card.read_bytes().decode())
        self.assertEqual([k.pref for k in profile.keys], [None, 1])
        self.assertEqual(len(profile.revocations), 1)
        result = validate_profile(profile)
        self.assertEqual((result.errors, result.warnings), ([], []))
        self.assertTrue(priv.exists() and pub.exists())
        self.assertEqual(oct(priv.stat().st_mode & 0o777), "0o600")

    def test_add_refuses_to_overwrite_key_files(self):
        priv = self.dir / "n.pem"
        priv.write_text("keep")
        result = self.run_cli(
            "key",
            "add",
            str(self.card),
            "--priv",
            str(priv),
            "--pub",
            str(self.dir / "p.pem"),
        )
        self.assertEqual(result.exit_code, 1)
        self.assertEqual(priv.read_text(), "keep")

    def test_add_existing_public_key(self):
        result = self.run_cli(
            "key", "add", str(self.card), "--public-key", NEW, "--no-pref"
        )
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertFalse((self.dir / "private.pem").exists())
        keys = parse_vcard(self.card.read_bytes().decode()).keys
        self.assertEqual([(k.key_b64, k.pref) for k in keys], [(ALICE, 1), (NEW, None)])

    def test_add_to_output_file_leaves_the_original(self):
        before = self.card.read_bytes()
        out = self.dir / "out.vcf"
        result = self.run_cli(
            "key", "add", str(self.card), "--public-key", NEW, "-o", str(out)
        )
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(self.card.read_bytes(), before)
        self.assertEqual(len(parse_vcard(out.read_bytes().decode()).keys), 2)
