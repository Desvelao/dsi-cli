import unittest

from src.dsipy.core.canonical import normalize_vcard
from src.dsipy.vcard.escaping import fold_line, split_structured
from src.dsipy.vcard.parser import parse_vcard
from src.dsipy.vcard.serializer import (
    build_content,
    build_vcard_from_raw_lines,
    format_param_value,
    format_property,
)

KEY = "MCowBQYDK2VwAyEAOAiOTCroL1xFxoCKYaZJDTxhLOHaI1cURm/HSPvEy7s="


def line_of(content: str, prefix: str) -> str:
    """Unfolded content line that starts with ``prefix``."""
    unfolded = content.replace("\r\n ", "")
    return next(x for x in unfolded.split("\r\n") if x.startswith(prefix))


class FieldEscapingTests(unittest.TestCase):
    def test_text_fields_are_escaped_and_round_trip(self):
        content = build_content(
            fn="A, B; C\\ D\nE",
            nickname="n;n",
            lang="en,US",
            kind="org;x",
            email="a,b@example.com",
            note="line1\nline2, with; stuff",
        )
        profile = parse_vcard(content)
        self.assertEqual(profile.fn, "A, B; C\\ D\nE")
        self.assertEqual(profile.nickname, "n;n")
        self.assertEqual(profile.lang, "en,US")
        self.assertEqual(profile.kind, "org;x")
        self.assertEqual(profile.email, "a,b@example.com")
        self.assertEqual(profile.note, "line1\nline2, with; stuff")
        self.assertEqual(profile.errors, [])

    def test_n_string_keeps_separators_and_pads(self):
        content = build_content(n="Doe;John")
        self.assertIn("\r\nN:Doe;John;;;\r\n", content)

    def test_n_list_escapes_components(self):
        content = build_content(n=["Doe; Jr", "John, A", "", "Dr", ""])
        raw = line_of(content, "N:")
        self.assertEqual(raw, r"N:Doe\; Jr;John\, A;;Dr;")
        self.assertEqual(
            split_structured(raw[2:], ";"), ["Doe; Jr", "John, A", "", "Dr", ""]
        )

    def test_adr_string_and_list(self):
        self.assertEqual(
            line_of(build_content(adr=";;Street 1;City"), "ADR:"),
            "ADR:;;Street 1;City;;;",
        )
        raw = line_of(build_content(adr=["", "", "1; Main, St", "City"]), "ADR:")
        self.assertEqual(raw, r"ADR:;;1\; Main\, St;City;;;")
        self.assertEqual(split_structured(raw[4:], ";")[2], "1; Main, St")

    def test_categories_list_of_escaped_items(self):
        raw = line_of(build_content(categories=["a,b", "c;d", "e"]), "CATEGORIES:")
        self.assertEqual(raw, r"CATEGORIES:a\,b,c\;d,e")
        self.assertEqual(split_structured(raw[11:], ","), ["a,b", "c;d", "e"])
        self.assertEqual(
            line_of(build_content(categories="gamer,programmer"), "CATEGORIES:"),
            "CATEGORIES:gamer,programmer",
        )

    def test_gender_structured(self):
        raw = line_of(build_content(gender="O;non, binary"), "GENDER:")
        self.assertEqual(raw, "GENDER:O;non\\, binary")

    def test_uri_values_stay_raw(self):
        content = build_content(
            tel="tel:+1;ext=5", url="https://x.example/a,b;c", source="https://s/a,b"
        )
        self.assertIn("TEL:tel:+1;ext=5\r\n", content)
        self.assertIn("URL:https://x.example/a,b;c\r\n", content)

    def test_line_break_in_raw_field_rejected(self):
        with self.assertRaises(ValueError):
            build_content(tel="1\nFN:evil")
        with self.assertRaises(ValueError):
            build_content(custom_attributes={"X-A": "v\r\nFN:evil"})

    def test_line_break_in_escaped_field_cannot_inject(self):
        content = build_content(n="Doe\nFN:evil", categories="a\r\nFN:evil")
        self.assertIsNone(parse_vcard(content).fn)
        self.assertEqual(parse_vcard(content).errors, [])


class FoldingTests(unittest.TestCase):
    def test_short_line_not_folded(self):
        self.assertEqual(fold_line("A" * 75), "A" * 75)

    def test_fold_at_75_octets(self):
        folded = fold_line("A" * 200).split("\r\n")
        self.assertEqual(len(folded[0]), 75)
        for part in folded[1:]:
            self.assertTrue(part.startswith(" "))
            self.assertLessEqual(len(part.encode()), 75)
        self.assertEqual(
            "".join(p[1:] if i else p for i, p in enumerate(folded)), "A" * 200
        )

    def test_fold_respects_multibyte_characters(self):
        text = "N:" + "é€😀" * 60
        folded = fold_line(text)
        for part in folded.split("\r\n"):
            self.assertLessEqual(len(part.encode("utf-8")), 75)
            part.encode("utf-8").decode("utf-8")
        self.assertEqual(folded.replace("\r\n ", ""), text)

    def test_long_key_is_folded_and_parses_back(self):
        long_key = "A" * 300
        content = build_content(
            fn="X",
            note="ñ" * 100,
            keys=[{"alg": "ed25519", "key_b64": long_key, "encoding": "b"}],
        )
        for physical in content.split("\r\n"):
            self.assertLessEqual(len(physical.encode("utf-8")), 75)
        profile = parse_vcard(content)
        self.assertEqual(profile.keys[0].key_b64, long_key)
        self.assertEqual(profile.note, "ñ" * 100)
        # canonical form is unfolded
        self.assertIn(f":{long_key}\r\n", normalize_vcard(profile))

    def test_raw_lines_rebuild_keeps_lines_unfolded(self):
        source = (
            "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:X\r\nNOTE:"
            + "b" * 200
            + "\r\nEND:VCARD\r\n"
        )
        rebuilt = build_vcard_from_raw_lines(parse_vcard(source))
        self.assertEqual(rebuilt, source)
        self.assertEqual(parse_vcard(rebuilt).note, "b" * 200)


class KeyAndParamTests(unittest.TestCase):
    def test_key_without_encoding_defaults_to_b(self):
        content = build_content(keys=[{"alg": "ED25519", "key_b64": KEY}])
        self.assertEqual(
            line_of(content, "KEY"),
            f"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:{KEY}",
        )

    def test_key_missing_required_field_raises_value_error(self):
        with self.assertRaises(ValueError) as ctx:
            build_content(keys=[{"alg": "ed25519"}])
        self.assertIn("key_b64", str(ctx.exception))

    def test_note_language_uses_lang_or_default(self):
        self.assertIn(
            "NOTE;LANGUAGE=es-ES:hola", build_content(lang="es-ES", note="hola")
        )
        self.assertIn("NOTE;LANGUAGE=en-US:hi", build_content(note="hi"))

    def test_format_param_value(self):
        self.assertEqual(format_param_value("plain"), "plain")
        self.assertEqual(format_param_value("a;b"), '"a;b"')
        with self.assertRaises(ValueError):
            format_param_value('say "hi"')
        with self.assertRaises(ValueError):
            format_param_value("a\nb")
        self.assertEqual(format_property("X-A", [("P", "a:b")], "v"), 'X-A;P="a:b":v')


if __name__ == "__main__":
    unittest.main()
