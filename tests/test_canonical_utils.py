import unittest

from src.dsipy.core.canonical import (
    canonical_endorsement_string,
    canonical_feed_string,
    normalize_vcard,
)
from src.dsipy.core.utils import slugify
from src.dsipy.vcard.parser import parse_vcard

from .helpers import crlf, make_key

_, ALICE = make_key("alice")
_, BOB = make_key("bob")


class TestNormalizeVcard(unittest.TestCase):
    def normalize(self, *lines):
        return normalize_vcard(parse_vcard(crlf(*lines)))

    def test_orders_properties_and_uses_crlf(self):
        out = self.normalize(
            "BEGIN:VCARD",
            "VERSION:4.0",
            "SOURCE:https://a.example/x.vcf",
            "X-ZZZ:last",
            "FN:Alice",
            "X-AAA:first-unknown",
            "END:VCARD",
        )
        self.assertEqual(
            out,
            crlf(
                "BEGIN:VCARD",
                "VERSION:4.0",
                "FN:Alice",
                "SOURCE:https://a.example/x.vcf",
                "X-AAA:first-unknown",
                "X-ZZZ:last",
                "END:VCARD",
            ),
        )

    def test_uppercases_names_and_sorts_params(self):
        out = self.normalize(
            "BEGIN:VCARD",
            "VERSION:4.0",
            "fn:Alice",
            f"key;pref=1;alg=ed25519;type=public;encoding=b:{ALICE}",
            "END:VCARD",
        )
        self.assertIn(
            f"KEY;ALG=ed25519;ENCODING=b;PREF=1;TYPE=public:{ALICE}", out
        )
        self.assertIn("FN:Alice\r\n", out)

    def test_unfolds_lines(self):
        out = self.normalize(
            "BEGIN:VCARD", "VERSION:4.0", "NOTE:hello", " world", "END:VCARD"
        )
        self.assertIn("NOTE:helloworld\r\n", out)

    def test_key_and_revkey_sorted_by_value_others_keep_order(self):
        keys = sorted([ALICE, BOB])
        out = self.normalize(
            "BEGIN:VCARD",
            "VERSION:4.0",
            f"KEY;ENCODING=b:{keys[1]}",
            f"KEY;ENCODING=b:{keys[0]}",
            "EMAIL:b@example.com",
            "EMAIL:a@example.com",
            "END:VCARD",
        )
        lines = out.split("\r\n")
        self.assertLess(
            lines.index(f"KEY;ENCODING=b:{keys[0]}"),
            lines.index(f"KEY;ENCODING=b:{keys[1]}"),
        )
        self.assertLess(
            lines.index("EMAIL:b@example.com"), lines.index("EMAIL:a@example.com")
        )

    def test_idempotent_and_input_order_independent(self):
        a = self.normalize(
            "BEGIN:VCARD", "VERSION:4.0", "FN:A", "NOTE:n", "END:VCARD"
        )
        b = self.normalize(
            "BEGIN:VCARD", "VERSION:4.0", "NOTE:n", "FN:A", "END:VCARD"
        )
        self.assertEqual(a, b)
        self.assertEqual(normalize_vcard(parse_vcard(a)), a)

    def test_malformed_lines_are_rejected(self):
        profile = parse_vcard(
            crlf("BEGIN:VCARD", "VERSION:4.0", "no colon here", "END:VCARD")
        )
        self.assertTrue(profile.errors)
        with self.assertRaises(ValueError):
            normalize_vcard(profile)


class TestCanonicalStrings(unittest.TestCase):
    def test_endorsement_string(self):
        self.assertEqual(canonical_endorsement_string("QUJD"), b"endorse:QUJD")

    def test_feed_string(self):
        self.assertEqual(
            canonical_feed_string("d", "t", "x"), "d\nt\nx".encode("utf-8")
        )


class TestSlugify(unittest.TestCase):
    def test_table(self):
        cases = [
            ("Hello World", "hello-world"),
            ("Árbol Ñandú Über", "arbol-nandu-uber"),
            ("  --a  b--  ", "a-b"),
            ("a_b!!c", "a-b-c"),
            ("", ""),
            ("日本語", ""),
            ("../../etc/passwd", "etc-passwd"),
        ]
        for text, expected in cases:
            with self.subTest(text=text):
                self.assertEqual(slugify(text), expected)


if __name__ == "__main__":
    unittest.main()
