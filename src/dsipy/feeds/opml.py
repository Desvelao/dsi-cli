import re
from pathlib import Path
from typing import List, Optional

from opyml import OPML, Outline

from ..vcard.parser import parse_vcard

_CARD_RE = re.compile(r"BEGIN:VCARD.*?END:VCARD", re.IGNORECASE | re.DOTALL)


def _categories(category: str, tags: str) -> str:
    """OPML `category` is a comma-separated list: merge category and tags."""
    items: List[str] = []
    for value in (category, tags):
        for item in value.split(","):
            item = item.strip()
            if item and item not in items:
                items.append(item)
    return ",".join(items)


def generate_opml_from_vcards(
    vcard_files: List[Path], warnings: Optional[List[str]] = None
) -> str:
    """
    Reads vCard files and generates an OPML file with one outline per X-FEED.

    The `category` attribute holds the feed category and tags (comma-separated)
    and `language` the feed language, both only when present. Cards that cannot
    be read or parsed are skipped and reported in `warnings`.

    Args:
        vcard_files (List[Path]): List of paths to vCard files.
        warnings (Optional[List[str]]): Collects a message per skipped card.
    """
    if warnings is None:
        warnings = []
    opml = OPML()

    for vcard_file in vcard_files:
        try:
            text = vcard_file.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError) as e:
            warnings.append(f"Skipping {vcard_file}: {e}")
            continue

        cards = _CARD_RE.findall(text) or [text]
        for card in cards:
            try:
                profile = parse_vcard(card)
            except Exception as e:
                warnings.append(f"Skipping malformed vCard in {vcard_file}: {e}")
                continue
            if profile.errors:
                warnings.append(
                    f"Skipping malformed vCard in {vcard_file}: {profile.errors[0]}"
                )
                continue

            name = profile.fn or "Unknown"
            for feed in profile.feeds:
                if not feed.url:
                    continue
                opml.body.outlines.append(
                    Outline(
                        text=name,
                        title=name,
                        type="rss",
                        xml_url=feed.url,
                        category=_categories(feed.category, feed.tags) or None,
                        language=feed.language or None,
                    )
                )
    if len(opml.body.outlines) == 0:
        raise ValueError("No valid vCards with feed URLs found in the provided files.")
    return opml.to_xml()
