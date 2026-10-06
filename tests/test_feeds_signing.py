from datetime import datetime
import unittest

from src.dsipy.feeds.rss import RSSFeed, strip_cdata
from src.dsipy.feeds.signing import INVALID, UNSIGNED, VALID, verify_feed_items

from .helpers import make_key

PRIV, PUB_B64 = make_key("alice")
_, OTHER_B64 = make_key("mallory")


def build_feed(items, sign=True):
    return RSSFeed.build(
        "Blog",
        "https://alice.example",
        "Alice's blog",
        "Alice",
        "alice@example.com",
        "en",
        datetime(2026, 1, 2, 3, 4, 5),
        items,
        sign={"key": PRIV, "id": PUB_B64} if sign else None,
    )


PLAIN = {
    "id": "one",
    "title": "Hello, world",
    "date": datetime(2026, 1, 1, 10, 0, 0),
    "content": "First post & more <b>text</b>",
}
HTML = {
    "id": "two",
    "title": "Rich",
    "date": datetime(2026, 1, 2, 11, 30, 0),
    "content": "<![CDATA[<p>Hi <em>there</em></p>]]>",
}


class TestFeedSigning(unittest.TestCase):
    def keys(self):
        from src.dsipy.crypto.keys import load_public_key_b64_der

        return {PUB_B64: load_public_key_b64_der(PUB_B64)}

    def test_signature_element_follows_the_specification(self):
        xml = build_feed([PLAIN])
        self.assertIn(f'<signature alg="ed25519" key-id="{PUB_B64}">', xml)
        self.assertNotIn("keyId", xml)

    def test_signed_items_verify(self):
        results = verify_feed_items(build_feed([PLAIN, HTML]), self.keys())
        self.assertEqual([r.status for r in results], [VALID, VALID])

    def test_html_description_is_signed_without_the_cdata_wrapper(self):
        self.assertEqual(strip_cdata("<![CDATA[<p>x</p>]]>"), "<p>x</p>")
        self.assertEqual(strip_cdata("plain"), "plain")
        results = verify_feed_items(build_feed([HTML]), self.keys())
        self.assertEqual(results[0].status, VALID, results[0].reason)

    def test_tampered_item_is_invalid(self):
        xml = build_feed([PLAIN]).replace("Hello, world", "Hello, evil")
        [result] = verify_feed_items(xml, self.keys())
        self.assertEqual(result.status, INVALID)

    def test_unknown_key_is_invalid(self):
        [result] = verify_feed_items(build_feed([PLAIN]), {OTHER_B64: object()})
        self.assertEqual(result.status, INVALID)
        self.assertIn("no matching key", result.reason)

    def test_unsigned_items_are_reported(self):
        [result] = verify_feed_items(build_feed([PLAIN], sign=False), self.keys())
        self.assertEqual(result.status, UNSIGNED)

    def test_legacy_key_id_attribute_is_accepted(self):
        xml = build_feed([PLAIN]).replace('alg="ed25519" key-id=', "keyId=")
        [result] = verify_feed_items(xml, self.keys())
        self.assertEqual(result.status, VALID)

    def test_dtd_is_rejected(self):
        with self.assertRaises(ValueError):
            verify_feed_items(
                '<?xml version="1.0"?><!DOCTYPE x [<!ENTITY a "b">]><rss/>', {}
            )

    def test_invalid_xml_is_rejected(self):
        with self.assertRaises(ValueError):
            verify_feed_items("<rss><item>", {})


if __name__ == "__main__":
    unittest.main()


class TestRSSRoundTrip(unittest.TestCase):
    MEDIA_NS = "http://search.yahoo.com/mrss/"

    def test_feed_with_images_is_namespace_well_formed_and_verifies(self):
        import xml.etree.ElementTree as ET
        from src.dsipy.crypto.keys import load_public_key_b64_der

        with_image = {**PLAIN, "image": "https://alice.example/a.png"}
        html_image = {**HTML, "image": "https://alice.example/b.jpg"}
        for sign in (True, False):
            xml = build_feed([with_image, html_image], sign=sign)
            self.assertIn(f'xmlns:media="{self.MEDIA_NS}"', xml)
            root = ET.fromstring(xml)  # raises on an unbound prefix
            media = root.findall(f".//{{{self.MEDIA_NS}}}content")
            self.assertEqual(len(media), 2)
            self.assertEqual(media[0].get("url"), "https://alice.example/a.png")
            self.assertEqual(media[0].get("medium"), "image")

            keys = {PUB_B64: load_public_key_b64_der(PUB_B64)}
            results = verify_feed_items(xml, keys)
            expected = VALID if sign else UNSIGNED
            self.assertEqual([r.status for r in results], [expected, expected])


class TestCDATAAndSignConfig(unittest.TestCase):
    def test_text_around_cdata_block_is_escaped(self):
        import xml.etree.ElementTree as ET

        item = {
            **PLAIN,
            "content": "a & b < c <![CDATA[<p>x & y</p>]]> d & e < f",
        }
        xml = build_feed([item], sign=False)
        root = ET.fromstring(xml)  # raises if malformed
        desc = root.find(".//item/description").text
        self.assertEqual(desc, "a & b < c <p>x & y</p> d & e < f")

    def test_signed_feed_with_cdata_description_verifies(self):
        from src.dsipy.crypto.keys import load_public_key_b64_der

        xml = build_feed([PLAIN, HTML])
        keys = {PUB_B64: load_public_key_b64_der(PUB_B64)}
        results = verify_feed_items(xml, keys)
        self.assertEqual([r.status for r in results], [VALID, VALID])

    def test_sign_config_without_id_raises_value_error(self):
        with self.assertRaises(ValueError):
            RSSFeed.build(
                "B",
                "https://a.example",
                "d",
                "A",
                "a@e.com",
                "en",
                datetime(2026, 1, 2),
                [PLAIN],
                sign={"key": PRIV},
            )

    def test_partial_sign_config_raises_value_error(self):
        for sign in ({"key": PRIV, "id": None}, {"key": None, "id": PUB_B64}):
            with self.assertRaises(ValueError):
                RSSFeed.build(
                    "B",
                    "https://a.example",
                    "d",
                    "A",
                    "a@e.com",
                    "en",
                    datetime(2026, 1, 2),
                    [PLAIN],
                    sign=sign,
                )
