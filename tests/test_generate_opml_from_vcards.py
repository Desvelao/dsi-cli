import tempfile
import unittest
from pathlib import Path

from src.dsipy.feeds.opml import generate_opml_from_vcards


class TestGenerateOpmlFromVcards(unittest.TestCase):
    def test_generates_opml_when_vcards_have_x_feed(self):
        vcard_content = """BEGIN:VCARD
VERSION:4.0
FN:Alice Example
X-FEED:https://example.com/feed.xml
END:VCARD
"""

        with tempfile.TemporaryDirectory() as tmp_dir:
            vcard_path = Path(tmp_dir) / "alice.vcf"
            vcard_path.write_text(vcard_content, encoding="utf-8")

            opml_xml = generate_opml_from_vcards([vcard_path])

        self.assertIn("<opml", opml_xml)
        self.assertIn("Alice Example", opml_xml)
        self.assertIn("https://example.com/feed.xml", opml_xml)

    def test_raises_when_no_vcard_contains_feed_url(self):
        vcard_content = """BEGIN:VCARD
VERSION:4.0
FN:Bob Example
END:VCARD
"""

        with tempfile.TemporaryDirectory() as tmp_dir:
            vcard_path = Path(tmp_dir) / "bob.vcf"
            vcard_path.write_text(vcard_content, encoding="utf-8")

            with self.assertRaises(ValueError):
                generate_opml_from_vcards([vcard_path])

    def test_raises_when_no_vcards_provided(self):
        with self.assertRaises(ValueError):
            generate_opml_from_vcards([])

    def test_generates_opml_with_multiple_vcards(self):
        vcard_content_1 = """BEGIN:VCARD
VERSION:4.0
FN:Alice Example
X-FEED:https://example.com/feed1.xml
END:VCARD
"""
        vcard_content_2 = """BEGIN:VCARD
VERSION:4.0
FN:Bob Example
X-FEED:https://example.com/feed2.xml
END:VCARD
"""

        with tempfile.TemporaryDirectory() as tmp_dir:
            vcard_path_1 = Path(tmp_dir) / "alice.vcf"
            vcard_path_1.write_text(vcard_content_1, encoding="utf-8")
            vcard_path_2 = Path(tmp_dir) / "bob.vcf"
            vcard_path_2.write_text(vcard_content_2, encoding="utf-8")

            opml_xml = generate_opml_from_vcards([vcard_path_1, vcard_path_2])

        self.assertIn("<opml", opml_xml)
        self.assertIn("Alice Example", opml_xml)
        self.assertIn("Bob Example", opml_xml)
        self.assertIn("https://example.com/feed1.xml", opml_xml)
        self.assertIn("https://example.com/feed2.xml", opml_xml)

    def _opml(self, *contents, warnings=None):
        with tempfile.TemporaryDirectory() as tmp_dir:
            paths = []
            for i, content in enumerate(contents):
                path = Path(tmp_dir) / f"c{i}.vcf"
                path.write_text(content, encoding="utf-8")
                paths.append(path)
            return generate_opml_from_vcards(paths, warnings)

    def test_multiple_feeds_in_one_card_emit_one_outline_each(self):
        xml = self._opml(
            "BEGIN:VCARD\nVERSION:4.0\nFN:Alice\n"
            "X-FEED:https://e.com/a.xml\n"
            "X-FEED;LANGUAGE=es-ES:https://e.com/b.xml\nEND:VCARD\n"
        )
        self.assertEqual(xml.count("<outline"), 2)
        self.assertIn("https://e.com/a.xml", xml)
        self.assertIn("https://e.com/b.xml", xml)
        self.assertIn('type="rss"', xml)

    def test_category_language_and_tags_attributes(self):
        xml = self._opml(
            "BEGIN:VCARD\nVERSION:4.0\nFN:Alice\n"
            'X-FEED;LANGUAGE=es-ES;CATEGORY=news;TAGS="tech,ai":https://e.com/a.xml\n'
            "X-FEED:https://e.com/plain.xml\nEND:VCARD\n"
        )
        self.assertIn('language="es-ES"', xml)
        self.assertIn('category="news,tech,ai"', xml)
        self.assertEqual(xml.count("language="), 1)
        self.assertEqual(xml.count("category="), 1)

    def test_malformed_card_is_skipped_with_warning(self):
        warnings = []
        xml = self._opml(
            "BEGIN:VCARD\nVERSION:4.0\nFN:Bad\nthis is not a valid line\n"
            "X-FEED:https://e.com/bad.xml\nEND:VCARD\n",
            "BEGIN:VCARD\nVERSION:4.0\nFN:Good\nX-FEED:https://e.com/good.xml\nEND:VCARD\n",
            warnings=warnings,
        )
        self.assertIn("https://e.com/good.xml", xml)
        self.assertNotIn("bad.xml", xml)
        self.assertEqual(len(warnings), 1)

    def test_multiple_cards_in_one_file(self):
        xml = self._opml(
            "BEGIN:VCARD\nFN:A\nX-FEED:https://e.com/1.xml\nEND:VCARD\n"
            "BEGIN:VCARD\nFN:B\nX-FEED:https://e.com/2.xml\nEND:VCARD\n"
        )
        self.assertEqual(xml.count("<outline"), 2)


if __name__ == "__main__":
    unittest.main()
