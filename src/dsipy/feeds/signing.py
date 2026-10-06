"""Verification of signed RSS items (RFC section 4)."""

from dataclasses import dataclass
from typing import Dict, List, Optional
import xml.etree.ElementTree as ET

from ..crypto.verification import verify_feed_signature

VALID = "valid"
INVALID = "invalid"
UNSIGNED = "unsigned"


@dataclass
class FeedItemResult:
    title: str
    guid: Optional[str]
    status: str  # VALID, INVALID or UNSIGNED
    reason: str = ""


def verify_feed_items(xml_text: str, keys: Dict[str, object]) -> List[FeedItemResult]:
    """Verify every item of an RSS document.

    Args:
        xml_text: the RSS document.
        keys: public keys by Key-ID (their Base64 DER value).

    Raises:
        ValueError: if the document is not well-formed RSS or declares a DTD.
    """
    if "<!DOCTYPE" in xml_text or "<!ENTITY" in xml_text:
        raise ValueError("DTD and entity declarations are not allowed in feeds")
    try:
        root = ET.fromstring(xml_text)
    except ET.ParseError as e:
        raise ValueError(f"Invalid XML: {e}") from e

    results = []
    for item in root.iter("item"):
        title = item.findtext("title") or ""
        guid = item.findtext("guid")
        signature = item.find("signature")
        if signature is None or not (signature.text or "").strip():
            results.append(FeedItemResult(title, guid, UNSIGNED))
            continue

        key_id = signature.get("key-id") or signature.get("keyId")
        algorithm = signature.get("alg")
        if algorithm and algorithm != "ed25519":
            results.append(
                FeedItemResult(title, guid, INVALID, f"unsupported alg '{algorithm}'")
            )
            continue
        key = keys.get(key_id) if key_id else None
        if key is None:
            results.append(
                FeedItemResult(
                    title, guid, INVALID, f"no matching key for key-id '{key_id}'"
                )
            )
            continue

        valid = verify_feed_signature(
            key,
            item.findtext("pubDate") or "",
            title,
            item.findtext("description") or "",
            signature.text.strip(),
        )
        results.append(
            FeedItemResult(
                title,
                guid,
                VALID if valid else INVALID,
                "" if valid else "bad signature",
            )
        )
    return results
