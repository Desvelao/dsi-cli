import unittest

from src.dsipy.core.identity import VCard
from src.dsipy.core.validator import validate_profile
from src.dsipy.vcard.escaping import escape_text, unescape_text
from src.dsipy.vcard.parser import parse_vcard
from src.dsipy.vcard.serializer import build_content, build_vcard_from_raw_lines

ALICE = "MCowBQYDK2VwAyEAOAiOTCroL1xFxoCKYaZJDTxhLOHaI1cURm/HSPvEy7s="
BOB = "MCowBQYDK2VwAyEA3XVgQP3VFF4r+YMtJk3QgOSz5zAWvfZXS0zYfqppf14="
SIG = "0c" * 64


def crlf(*lines):
    return "\r\n".join(lines) + "\r\n"


EXAMPLE = crlf(
    "BEGIN:VCARD",
    "VERSION:4.0",
    "FN:Alice Example",
    f"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:{ALICE}",
    "REVKEY;TYPE=public;ALG=ed25519;REASON=rotated;DATE=20260221T110230Z;"
    f"ENCODING=b:{BOB}",
    "SOURCE:https://example.com/alice.vcf",
    "X-DSI-VERSION;FEATURES=tags,endorse:00",
    "X-FEED:https://example.com/alice/feed.xml",
    "X-FEED;LANGUAGE=es-ES;TAGS=nature,travel:https://example.com/alice/es.xml",
    "X-SOCIAL;PLATFORM=github:alice",
    "X-SOCIAL;PLATFORM=activitypub:https://social.example/@alice",
    f"X-ENDORSE;SIG={SIG};DATE=20260210T120000Z;CONFIDENCE=high;ENCODING=b:{BOB}",
    "X-ACME-UNKNOWN;FOO=bar:keep me",
    "END:VCARD",
)


class TestParseVcard(unittest.TestCase):
    def test_parses_the_dsi_properties(self):
        p = parse_vcard(EXAMPLE)
        self.assertEqual(p.version, "4.0")
        self.assertEqual(p.fn, "Alice Example")
        self.assertEqual(p.source, "https://example.com/alice.vcf")
        self.assertEqual(
            [(k.alg, k.key_b64, k.pref) for k in p.keys], [("ed25519", ALICE, 1)]
        )
        self.assertEqual(p.revocations[0].key_b64, BOB)
        self.assertEqual(p.revocations[0].reason, "rotated")
        self.assertEqual(p.revocations[0].date, "20260221T110230Z")
        e = p.endorsements[0]
        self.assertEqual(
            (e.endorsee_key_b64, e.signature_hex, e.date, e.confidence),
            (BOB, SIG, "20260210T120000Z", "high"),
        )
        self.assertEqual(p.dsi_version.revision, "00")
        self.assertEqual(p.dsi_version.features, ["tags", "endorse"])
        self.assertEqual(
            [(s.platform, s.value) for s in p.social],
            [
                ("github", "alice"),
                ("activitypub", "https://social.example/@alice"),
            ],
        )
        self.assertEqual(p.errors, [])

    def test_feeds_with_and_without_parameters(self):
        feeds = parse_vcard(EXAMPLE).feeds
        self.assertEqual(len(feeds), 2)
        self.assertEqual(feeds[0].url, "https://example.com/alice/feed.xml")
        self.assertEqual(feeds[0].language, "")
        self.assertEqual(feeds[1].language, "es-ES")
        self.assertEqual(feeds[1].tags, "nature,travel")

    def test_key_without_parameters_is_recognised(self):
        p = parse_vcard(crlf("BEGIN:VCARD", f"KEY:{ALICE}", "END:VCARD"))
        self.assertEqual(p.keys[0].key_b64, ALICE)
        self.assertEqual(p.keys[0].alg, "")

    def test_property_and_parameter_names_are_case_insensitive(self):
        p = parse_vcard(
            crlf(
                "BEGIN:VCARD",
                f"key;type=public;alg=ED25519;pref=2:{ALICE}",
                "END:VCARD",
            )
        )
        self.assertEqual((p.keys[0].alg, p.keys[0].pref), ("ed25519", 2))

    def test_folded_lines_are_joined(self):
        p = parse_vcard(
            "BEGIN:VCARD\r\nFN:Alice\r\nNOTE:a very long\r\n  folded note\r\n"
            f"KEY;ALG=ed25519:{ALICE[:20]}\r\n {ALICE[20:]}\r\nEND:VCARD\r\n"
        )
        self.assertEqual(p.note, "a very long folded note")
        self.assertEqual(p.keys[0].key_b64, ALICE)

    def test_text_values_are_unescaped(self):
        p = parse_vcard(
            crlf(
                "BEGIN:VCARD",
                r"FN:Alice\, Example\; Jr",
                r"NOTE:one\ntwo \\ end",
                "END:VCARD",
            )
        )
        self.assertEqual(p.fn, "Alice, Example; Jr")
        self.assertEqual(p.note, "one\ntwo \\ end")

    def test_structured_values_are_kept_raw(self):
        p = parse_vcard(crlf("BEGIN:VCARD", r"N:Example;Alice;;;", "END:VCARD"))
        self.assertEqual(p.n, "Example;Alice;;;")

    def test_value_of_property_with_parameters(self):
        p = parse_vcard(
            crlf("BEGIN:VCARD", "NOTE;LANGUAGE=en-US:hello: world", "END:VCARD")
        )
        self.assertEqual(p.note, "hello: world")

    def test_quoted_parameter_values_may_contain_separators(self):
        p = parse_vcard(
            crlf("BEGIN:VCARD", 'X-FEED;LANGUAGE="en;US":https://e.com/a', "END:VCARD")
        )
        self.assertEqual(p.feeds[0].language, "en;US")
        self.assertEqual(p.feeds[0].url, "https://e.com/a")

    def test_invalid_pref_is_reported_not_fatal(self):
        p = parse_vcard(crlf("BEGIN:VCARD", f"KEY;PREF=high:{ALICE}", "END:VCARD"))
        self.assertIsNone(p.keys[0].pref)
        self.assertEqual(len(p.errors), 1)

    def test_malformed_line_is_reported_not_fatal(self):
        p = parse_vcard(crlf("BEGIN:VCARD", "FN:Alice", "garbage line", "END:VCARD"))
        self.assertEqual(p.fn, "Alice")
        self.assertEqual(len(p.errors), 1)

    def test_raw_text_is_preserved(self):
        self.assertEqual(VCard(text=EXAMPLE).to_string(), EXAMPLE)


class TestVCardEditing(unittest.TestCase):
    def test_add_line_keeps_crlf_and_unknown_properties(self):
        card = VCard(text=EXAMPLE)
        card.add_line(f"X-ENDORSE;SIG={SIG};ENCODING=b:{ALICE}")
        text = card.to_string()
        self.assertTrue(text.startswith(EXAMPLE[: -len("END:VCARD\r\n")]))
        self.assertTrue(text.endswith(f"ENCODING=b:{ALICE}\r\nEND:VCARD\r\n"))
        self.assertNotIn("\n", text.replace("\r\n", ""))
        self.assertIn("X-ACME-UNKNOWN;FOO=bar:keep me\r\n", text)
        self.assertEqual(len(card.profile.endorsements), 2)

    def test_add_line_rejects_line_breaks(self):
        with self.assertRaises(ValueError):
            VCard(text=EXAMPLE).add_line("X-FOO:a\r\nX-EVIL:b")

    def test_rebuild_from_raw_lines_keeps_every_property(self):
        rebuilt = build_vcard_from_raw_lines(parse_vcard(EXAMPLE))
        self.assertEqual(rebuilt, EXAMPLE)


class TestBuildContent(unittest.TestCase):
    def test_uses_crlf_and_escapes_text(self):
        text = build_content(
            fn="Alice, Example; Jr", note="line1\nline2", source="https://e.com/a.vcf"
        )
        self.assertTrue(text.endswith("END:VCARD\r\n"))
        self.assertNotIn("\n", text.replace("\r\n", ""))
        self.assertIn(r"FN:Alice\, Example\; Jr", text)
        self.assertIn(r"line1\nline2", text)

    def test_round_trip(self):
        p = parse_vcard(
            build_content(
                fn="Alice, Example", note="a\nb", source="https://e.com/a.vcf"
            )
        )
        self.assertEqual(p.fn, "Alice, Example")
        self.assertEqual(p.note, "a\nb")
        self.assertEqual(p.source, "https://e.com/a.vcf")
        self.assertEqual(p.errors, [])

    def test_line_breaks_cannot_inject_properties(self):
        with self.assertRaises(ValueError):
            build_content(fn="x", url="https://e.com\r\nX-EVIL:1")
        with self.assertRaises(ValueError):
            build_content(fn="x", custom_attributes={"X-A": "v\nX-EVIL:1"})
        text = build_content(fn="Alice\r\nX-EVIL:1")
        self.assertNotIn("\r\nX-EVIL", text)

    def test_key_property(self):
        text = build_content(
            fn="A",
            keys=[{"alg": "ed25519", "key_b64": ALICE, "pref": 1, "encoding": "b"}],
        )
        self.assertEqual(parse_vcard(text).keys[0].key_b64, ALICE)


class TestEscaping(unittest.TestCase):
    def test_round_trip(self):
        for value in ["plain", "a,b;c", "back\\slash", "multi\nline", "\\n literal"]:
            self.assertEqual(unescape_text(escape_text(value)), value)


if __name__ == "__main__":
    unittest.main()


class TestFileRoundTrip(unittest.TestCase):
    def test_crlf_file_is_preserved_byte_for_byte(self):
        import tempfile
        from pathlib import Path

        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "alice.vcf"
            path.write_bytes(EXAMPLE.encode("utf-8"))
            card = VCard(path=path)
            self.assertNotIn(
                "line-endings",
                [i.code for i in validate_profile(card.profile).warnings],
            )
            card.to_file()
            self.assertEqual(path.read_bytes(), EXAMPLE.encode("utf-8"))


class PreferredKeyTests(unittest.TestCase):
    def preferred(self, *lines):
        return VCard(
            text=crlf("BEGIN:VCARD", "VERSION:4.0", "FN:A", *lines, "END:VCARD")
        ).get_preferred_key()

    def test_lowest_pref_wins(self):
        key = self.preferred(
            f"KEY;TYPE=public;ALG=ed25519;PREF=2;ENCODING=b:{ALICE}",
            f"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:{BOB}",
        )
        self.assertEqual(key.key_b64, BOB)

    def test_revoked_key_is_skipped(self):
        key = self.preferred(
            f"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:{ALICE}",
            f"KEY;TYPE=public;ALG=ed25519;PREF=2;ENCODING=b:{BOB}",
            f"REVKEY;TYPE=public;ALG=ed25519;ENCODING=b:{ALICE}",
        )
        self.assertEqual(key.key_b64, BOB)

    def test_only_revoked_key_returns_none(self):
        key = self.preferred(
            f"KEY;TYPE=public;ALG=ed25519;ENCODING=b:{ALICE}",
            f"REVKEY;TYPE=public;ALG=ed25519;ENCODING=b:{ALICE}",
        )
        self.assertIsNone(key)

    def test_non_ed25519_key_is_skipped(self):
        key = self.preferred(
            f"KEY;TYPE=public;ALG=rsa;PREF=1;ENCODING=b:{ALICE}",
            f"KEY;TYPE=public;ALG=ed25519;PREF=2;ENCODING=b:{BOB}",
        )
        self.assertEqual(key.key_b64, BOB)


class TestVcardFraming(unittest.TestCase):
    def test_valid_single_card_has_no_errors(self):
        self.assertEqual(parse_vcard(EXAMPLE).errors, [])

    def test_multiple_cards_parse_only_the_first(self):
        text = crlf(
            "BEGIN:VCARD",
            "VERSION:4.0",
            "FN:First",
            "END:VCARD",
            "BEGIN:VCARD",
            "VERSION:4.0",
            "FN:Second",
            "NOTE:x",
            "END:VCARD",
        )
        p = parse_vcard(text)
        self.assertEqual(p.fn, "First")
        self.assertIsNone(p.note)
        self.assertEqual(p.errors, ["multiple vCards found; only the first is parsed"])

    def test_second_card_after_unterminated_first_is_not_merged(self):
        text = crlf(
            "BEGIN:VCARD",
            "VERSION:4.0",
            "FN:First",
            "BEGIN:VCARD",
            "FN:Second",
        )
        p = parse_vcard(text)
        self.assertEqual(p.fn, "First")
        self.assertIn("multiple vCards found; only the first is parsed", p.errors)
        self.assertIn("missing END:VCARD", p.errors)

    def test_missing_end(self):
        p = parse_vcard(crlf("BEGIN:VCARD", "VERSION:4.0", "FN:A"))
        self.assertEqual(p.errors, ["missing END:VCARD"])

    def test_missing_begin(self):
        p = parse_vcard(crlf("VERSION:4.0", "FN:A", "END:VCARD"))
        self.assertEqual(p.errors, ["missing BEGIN:VCARD"])
        self.assertEqual(p.fn, "A")

    def test_unframed_snippet_is_tolerated(self):
        self.assertEqual(parse_vcard(crlf("FN:A")).errors, [])
