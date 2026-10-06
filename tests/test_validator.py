import unittest

from src.dsipy.core.validator import validate_profile
from src.dsipy.crypto.signatures import sign_endorsement
from src.dsipy.endorsements.verify import INVALID, VALID, verify_endorsements
from src.dsipy.vcard.parser import parse_vcard

from .helpers import crlf, dsi_vcard, make_key

ALICE_PRIV, ALICE = make_key("alice")
_, BOB = make_key("bob")
_, OLD = make_key("old")


def endorse_line(signer, endorsee_b64, date="20260210T120000Z", extra=""):
    sig = sign_endorsement(signer, endorsee_b64)
    return f"X-ENDORSE;SIG={sig};DATE={date}{extra};ENCODING=b:{endorsee_b64}"


def codes(result):
    return {i.code for i in result.errors}, {i.code for i in result.warnings}


def validate(text):
    return validate_profile(parse_vcard(text))


class TestValidator(unittest.TestCase):
    def test_complete_valid_vcard(self):
        text = dsi_vcard(
            f"REVKEY;TYPE=public;ALG=ed25519;REASON=rotated;DATE=20260221T110230Z;ENCODING=b:{OLD}",
            "X-DSI-VERSION;FEATURES=tags,endorse:00",
            "X-FEED;LANGUAGE=en-US;TAGS=nature:https://alice.example/feed.xml",
            "X-SOCIAL;PLATFORM=github:alice",
            endorse_line(ALICE_PRIV, BOB, extra=";CONFIDENCE=high"),
        )
        result = validate(text)
        self.assertEqual(result.errors, [])
        self.assertEqual(result.warnings, [])
        self.assertTrue(result.valid)

    def test_json_shape(self):
        data = validate(dsi_vcard()).to_dict()
        self.assertEqual(data, {"valid": True, "errors": [], "warnings": []})

    def test_missing_required_properties(self):
        errors, _ = codes(validate(crlf("BEGIN:VCARD", "END:VCARD")))
        self.assertTrue({"version-missing", "fn-missing", "source-missing"} <= errors)

    def test_wrong_version(self):
        text = dsi_vcard().replace("VERSION:4.0", "VERSION:3.0")
        self.assertIn("version", codes(validate(text))[0])

    def test_source_must_be_absolute_and_https_recommended(self):
        errors, _ = codes(validate(dsi_vcard(source="alice.vcf")))
        self.assertIn("source-invalid", errors)
        errors, warnings = codes(
            validate(dsi_vcard(source="http://alice.example/a.vcf"))
        )
        self.assertNotIn("source-invalid", errors)
        self.assertIn("source-http", warnings)

    def test_key_problems(self):
        text = dsi_vcard(
            "KEY;TYPE=public;ALG=rsa;ENCODING=b:AAAA",
            "KEY;TYPE=public;ALG=ed25519;ENCODING=b:not base64!!",
            keys=False,
        )
        errors, _ = codes(validate(text))
        self.assertIn("key-alg", errors)
        self.assertIn("key-invalid", errors)

    def test_missing_key_is_a_warning(self):
        result = validate(dsi_vcard(keys=False))
        self.assertTrue(result.valid)
        self.assertIn("key-missing", codes(result)[1])

    def test_two_preferred_keys(self):
        text = dsi_vcard(f"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:{BOB}")
        self.assertIn("key-pref", codes(validate(text))[0])

    def test_preferred_key_that_is_revoked(self):
        text = dsi_vcard(
            f"REVKEY;ALG=ed25519;REASON=compromised;DATE=20260221T110230Z;ENCODING=b:{ALICE}"
        )
        self.assertIn("key-revoked", codes(validate(text))[0])

    def test_revkey_checks(self):
        text = dsi_vcard(
            f"REVKEY;ALG=ed25519;REASON=whatever;DATE=2026-02-21;ENCODING=b:{OLD}",
            f"REVKEY;ALG=ed25519;ENCODING=b:{BOB}",
        )
        errors, warnings = codes(validate(text))
        self.assertTrue({"revkey-reason", "revkey-date"} <= errors)
        self.assertTrue({"revkey-reason-missing", "revkey-date-missing"} <= warnings)

    def test_endorsement_with_bad_signature(self):
        text = dsi_vcard(
            f"X-ENDORSE;SIG={'0' * 128};DATE=20260210T120000Z;ENCODING=b:{BOB}"
        )
        self.assertIn("endorse-signature", codes(validate(text))[0])

    def test_endorsement_signed_by_someone_else(self):
        other, _ = make_key("mallory")
        text = dsi_vcard(endorse_line(other, BOB))
        self.assertIn("endorse-signature", codes(validate(text))[0])

    def test_endorsement_format_errors(self):
        text = dsi_vcard(
            f"X-ENDORSE;SIG=ABCD;DATE=today;CONFIDENCE=huge;ENCODING=b:{BOB}"
        )
        errors, _ = codes(validate(text))
        self.assertTrue(
            {"endorse-sig-format", "endorse-date", "endorse-confidence"} <= errors
        )

    def test_duplicate_endorsement_is_a_warning(self):
        line = endorse_line(ALICE_PRIV, BOB)
        result = validate(dsi_vcard(line, line))
        self.assertTrue(result.valid)
        self.assertIn("endorse-duplicate", codes(result)[1])

    def test_unverifiable_endorsement_without_keys(self):
        text = dsi_vcard(f"X-ENDORSE;SIG={'a' * 128};ENCODING=b:{BOB}", keys=False)
        result = validate(text)
        self.assertIn("endorse-unverifiable", codes(result)[1])

    def test_feed_checks(self):
        text = dsi_vcard(
            "X-FEED:feed.xml",
            "X-FEED;LANGUAGE=not_a_tag:https://a.example/f.xml",
            "X-FEED;TAGS=a b:https://a.example/g.xml",
            "X-FEED:ftp://a.example/h.xml",
            "X-FEED:http://a.example/i.xml",
        )
        errors, warnings = codes(validate(text))
        self.assertTrue(
            {"feed-invalid", "feed-language", "feed-tags", "feed-scheme"} <= errors
        )
        self.assertIn("feed-http", warnings)

    def test_social_checks(self):
        errors, _ = codes(
            validate(
                dsi_vcard("X-SOCIAL;PLATFORM=GitHub1:alice", "X-SOCIAL;PLATFORM=x:")
            )
        )
        self.assertTrue({"social-platform", "social-value"} <= errors)

    def test_dsi_version_checks(self):
        errors, _ = codes(validate(dsi_vcard("X-DSI-VERSION;FEATURES=a b:0")))
        self.assertTrue({"dsi-version", "dsi-version-feature"} <= errors)

    def test_malformed_lines_are_errors(self):
        text = dsi_vcard("this is not a property")
        self.assertIn("malformed-line", codes(validate(text))[0])

    def test_lf_only_line_endings_warn(self):
        text = dsi_vcard().replace("\r\n", "\n")
        self.assertIn("line-endings", codes(validate(text))[1])

    def test_missing_end_marker(self):
        text = dsi_vcard().replace("END:VCARD\r\n", "")
        self.assertIn("structure", codes(validate(text))[0])


class TestEndorsementVerification(unittest.TestCase):
    def verify(self, *lines):
        return verify_endorsements(parse_vcard(dsi_vcard(*lines)))

    def test_valid(self):
        [result] = self.verify(endorse_line(ALICE_PRIV, BOB))
        self.assertEqual((result.status, result.signer_key_b64), (VALID, ALICE))

    def test_tampered_endorsee(self):
        sig = sign_endorsement(ALICE_PRIV, BOB)
        [result] = self.verify(f"X-ENDORSE;SIG={sig};ENCODING=b:{OLD}")
        self.assertEqual(result.status, INVALID)

    def test_old_key_before_and_after_rotation(self):
        old_priv, old_b64 = make_key("old")
        revkey = f"REVKEY;ALG=ed25519;REASON=rotated;DATE=20260221T000000Z;ENCODING=b:{old_b64}"
        key = f"KEY;TYPE=public;ALG=ed25519;ENCODING=b:{old_b64}"
        before = self.verify(
            key, revkey, endorse_line(old_priv, BOB, "20260210T120000Z")
        )
        after = self.verify(
            key, revkey, endorse_line(old_priv, BOB, "20260301T120000Z")
        )
        undated = self.verify(
            key,
            revkey,
            f"X-ENDORSE;SIG={sign_endorsement(old_priv, BOB)};ENCODING=b:{BOB}",
        )
        self.assertEqual(before[0].status, VALID)
        self.assertEqual(after[0].status, INVALID)
        self.assertEqual(undated[0].status, INVALID)

    def test_compromised_key_never_validates(self):
        old_priv, old_b64 = make_key("old")
        results = self.verify(
            f"KEY;TYPE=public;ALG=ed25519;ENCODING=b:{old_b64}",
            f"REVKEY;ALG=ed25519;REASON=compromised;DATE=20260221T000000Z;ENCODING=b:{old_b64}",
            endorse_line(old_priv, BOB, "20260101T000000Z"),
        )
        self.assertEqual(results[0].status, INVALID)

    def test_deprecated_key_still_validates(self):
        old_priv, old_b64 = make_key("old")
        results = self.verify(
            f"KEY;TYPE=public;ALG=ed25519;ENCODING=b:{old_b64}",
            f"REVKEY;ALG=ed25519;REASON=deprecated;DATE=20200101T000000Z;ENCODING=b:{old_b64}",
            endorse_line(old_priv, BOB, "20260101T000000Z"),
        )
        self.assertEqual(results[0].status, VALID)


if __name__ == "__main__":
    unittest.main()
