import re

from ..core.canonical import canonical_endorsement_string, canonical_feed_string

_SIGNATURE_HEX_RE = re.compile(r"[0-9a-f]+")


def decode_signature_hex(signature_hex: str) -> bytes:
    """Decode a lowercase hex signature; reject uppercase, whitespace and odd length."""
    if not _SIGNATURE_HEX_RE.fullmatch(signature_hex or ""):
        raise ValueError("signature must be lowercase hexadecimal")
    return bytes.fromhex(signature_hex)


def verify_endorsement_signature(
    public_key, endorsee_key_b64: str, signature_hex: str
) -> bool:
    canonical = canonical_endorsement_string(endorsee_key_b64)
    try:
        public_key.verify(decode_signature_hex(signature_hex), canonical)
        return True
    except Exception:
        return False


def verify_feed_signature(
    public_key, pub_date: str, title: str, description_plain: str, signature_hex: str
) -> bool:
    """Verifies a feed item signature."""
    canonical = canonical_feed_string(pub_date, title, description_plain)
    try:
        public_key.verify(decode_signature_hex(signature_hex), canonical)
        return True
    except Exception:
        return False
