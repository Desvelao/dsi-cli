from __future__ import annotations


def canonical_endorsement_string(endorsee_key_b64: str) -> bytes:
    """
    Canonical string defined in the RFC:
        endorse:<BASE64_DER_KEY>
    """
    return f"endorse:{endorsee_key_b64}".encode("utf-8")


def canonical_feed_string(pub_date: str, title: str, description_plain: str) -> bytes:
    """
    Canonical string defined in the RFC:
        <pubDate>\n<title>\n<description_plain>
    """
    return f"{pub_date}\n{title}\n{description_plain}".encode("utf-8")


# ------------------------------------------------------------------
# vCard normalization
# ------------------------------------------------------------------

_PROPERTY_ORDER = [
    "FN",
    "N",
    "NICKNAME",
    "PHOTO",
    "BDAY",
    "ANNIVERSARY",
    "GENDER",
    "ADR",
    "TEL",
    "EMAIL",
    "IMPP",
    "LANG",
    "KIND",
    "CATEGORIES",
    "NOTE",
    "URL",
    "SOURCE",
    "KEY",
    "REVKEY",
    "X-DSI-VERSION",
    "X-FEED",
    "X-SOCIAL",
    "X-ENDORSE",
]


def normalize_vcard(profile) -> str:
    """Return the deterministic form of a parsed vCard.

    The result uses CRLF line endings, unfolded lines, upper-case property and
    parameter names, parameters sorted by name, and properties in a fixed order
    (known properties first, then the rest by name; equal properties keep their
    original order, except KEY and REVKEY, which are sorted by value).

    Raises:
        ValueError: if the vCard has malformed lines.
    """
    from ..vcard.serializer import format_property

    if profile.errors:
        raise ValueError(
            "Cannot normalize a vCard with malformed lines: "
            + "; ".join(profile.errors)
        )

    entries = []
    for index, line in enumerate(profile.raw_lines):
        name = line["name"]
        if name in ("BEGIN", "END", "VERSION"):
            continue
        rank = (
            _PROPERTY_ORDER.index(name)
            if name in _PROPERTY_ORDER
            else len(_PROPERTY_ORDER)
        )
        tie = line["raw_value"] if name in ("KEY", "REVKEY") else ""
        entries.append(
            (
                rank,
                name if rank == len(_PROPERTY_ORDER) else "",
                tie,
                index,
                format_property(
                    name,
                    sorted(line["params"]),
                    line["raw_value"],
                    line.get("group"),
                ),
            )
        )
    entries.sort(key=lambda e: e[:4])

    lines = ["BEGIN:VCARD", f"VERSION:{profile.version or '4.0'}"]
    lines.extend(entry[4] for entry in entries)
    lines.append("END:VCARD")
    return "\r\n".join(lines) + "\r\n"
