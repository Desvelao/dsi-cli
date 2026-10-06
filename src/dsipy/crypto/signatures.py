from ..core.canonical import canonical_endorsement_string, canonical_feed_string


def sign_endorsement(private_key, endorsee_key_b64: str) -> str:
    """
    Signs an endorsement and returns hex signature.
    """
    canonical = canonical_endorsement_string(endorsee_key_b64)
    sig = private_key.sign(canonical)
    return sig.hex()


def sign_feed_item(
    private_key, pub_date: str, title: str, description_plain: str
) -> str:
    """
    Signs a feed item and returns hex signature.
    """
    canonical = canonical_feed_string(pub_date, title, description_plain)
    sig = private_key.sign(canonical)
    return sig.hex()
