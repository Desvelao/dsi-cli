import unittest

from src.dsipy.core.model import Endorsement, Profile, PublicKey, RevokedKey
from src.dsipy.crypto.signatures import sign_endorsement
from src.dsipy.crypto.verification import verify_endorsement_signature
from src.dsipy.endorsements.verify import (
    INVALID,
    VALID,
    key_accepts_endorsement,
    parse_dsi_date,
    verify_endorsements,
)

from .helpers import make_key

OLD_PRIV, OLD = make_key("old")
_, BOB = make_key("bob")
REVOKED_AT = "20260221T000000Z"


def check(reason, revoked_date, signed_date):
    profile = Profile(revocations=[RevokedKey(OLD, reason, revoked_date)])
    return key_accepts_endorsement(
        profile, OLD, Endorsement(BOB, "00", date=signed_date)
    )[0]


class TestRevocationRules(unittest.TestCase):
    def test_table(self):
        cases = [
            # (name, reason, revoked_date, signed_date, accepted)
            (
                "compromised before",
                "compromised",
                REVOKED_AT,
                "20260101T000000Z",
                False,
            ),
            ("compromised no dates", "compromised", None, None, False),
            ("deprecated after", "deprecated", REVOKED_AT, "20270101T000000Z", True),
            ("deprecated no dates", "deprecated", None, None, True),
            ("rotated before", "rotated", REVOKED_AT, "20260220T235959Z", True),
            ("rotated after", "rotated", REVOKED_AT, "20260221T000001Z", False),
            ("rotated equal boundary", "rotated", REVOKED_AT, REVOKED_AT, False),
            ("rotated missing signed date", "rotated", REVOKED_AT, None, False),
            (
                "rotated missing revoked date",
                "rotated",
                None,
                "20260101T000000Z",
                False,
            ),
            (
                "rotated malformed revoked date",
                "rotated",
                "2026-02-21",
                "20260101T000000Z",
                False,
            ),
            (
                "rotated malformed signed date",
                "rotated",
                REVOKED_AT,
                "yesterday",
                False,
            ),
            ("no reason before", None, REVOKED_AT, "20260101T000000Z", True),
            ("no reason after", None, REVOKED_AT, "20260301T000000Z", False),
        ]
        for name, reason, revoked, signed, expected in cases:
            with self.subTest(name):
                self.assertIs(check(reason, revoked, signed), expected)

    def test_unrevoked_key_accepted(self):
        accepted, reason = key_accepts_endorsement(
            Profile(), OLD, Endorsement(BOB, "00")
        )
        self.assertEqual((accepted, reason), (True, ""))

    def test_revocation_of_other_key_ignored(self):
        profile = Profile(revocations=[RevokedKey(BOB, "compromised", None)])
        self.assertTrue(
            key_accepts_endorsement(profile, OLD, Endorsement(BOB, "00"))[0]
        )

    def test_parse_dsi_date(self):
        self.assertIsNotNone(parse_dsi_date("20260221T000000Z"))
        for bad in (None, "", "2026-02-21", "20260221T000000"):
            with self.subTest(bad=bad):
                self.assertIsNone(parse_dsi_date(bad))


class TestSignatureHexStrictness(unittest.TestCase):
    def setUp(self):
        self.sig = sign_endorsement(OLD_PRIV, BOB)
        self.public = OLD_PRIV.public_key()

    def variants(self):
        spaced = " ".join(self.sig[i : i + 2] for i in range(0, len(self.sig), 2))
        return {
            "uppercase": self.sig.upper(),
            "inner whitespace": spaced,
            "leading space": " " + self.sig,
            "trailing newline": self.sig + "\n",
            "odd length": self.sig[:-1],
            "empty": "",
            "non-hex": "zz" * 64,
        }

    def test_lowercase_accepted(self):
        self.assertTrue(verify_endorsement_signature(self.public, BOB, self.sig))

    def test_verification_rejects_non_lowercase_hex(self):
        for name, sig in self.variants().items():
            with self.subTest(name):
                self.assertFalse(verify_endorsement_signature(self.public, BOB, sig))

    def test_verify_endorsements_rejects_non_lowercase_hex(self):
        for name, sig in self.variants().items():
            with self.subTest(name):
                profile = Profile(
                    keys=[PublicKey("ed25519", OLD, 1)],
                    endorsements=[Endorsement(BOB, sig)],
                )
                [result] = verify_endorsements(profile)
                self.assertEqual(result.status, INVALID)

    def test_verify_endorsements_accepts_lowercase(self):
        profile = Profile(
            keys=[PublicKey("ed25519", OLD, 1)],
            endorsements=[Endorsement(BOB, self.sig)],
        )
        self.assertEqual(verify_endorsements(profile)[0].status, VALID)


if __name__ == "__main__":
    unittest.main()
