"""Key rotation and revocation of a vCard (RFC sections 3.2.1 and 3.2.2)."""

from datetime import datetime, timezone
from typing import Optional

from ..crypto.keys import load_public_key_b64_der
from ..vcard.parser import parse_vcard
from ..vcard.serializer import format_property
from .model import DSI_DATE_FORMAT, REVOCATION_REASONS, Profile

# Backward-compatible alias
DATE_FORMAT = DSI_DATE_FORMAT


def _lines(profile: Profile) -> list:
    """Logical lines of the profile without BEGIN/END."""
    return [
        line for line in profile.raw_lines if line["name"] not in ("BEGIN", "END", None)
    ]


def _render(profile: Profile, lines: list) -> str:
    text = ["BEGIN:VCARD"]
    text.extend(
        (
            format_property(l["name"], l["params"], l["raw_value"], l.get("group"))
            if l.get("edited")
            else l["line"]
        )
        for l in lines
    )
    text.append("END:VCARD")
    return "\r\n".join(text) + "\r\n"


def _without_pref(line: dict) -> dict:
    params = [(n, v) for n, v in line["params"] if n != "PREF"]
    return {**line, "params": params, "edited": True}


def _revkey_line(
    key_b64: str, reason: str, when: datetime, alg: str = "ed25519"
) -> dict:
    return {
        "name": "REVKEY",
        "params": [
            ("TYPE", "public"),
            ("ALG", alg or "ed25519"),
            ("REASON", reason),
            ("DATE", when.strftime(DSI_DATE_FORMAT)),
            ("ENCODING", "b"),
        ],
        "raw_value": key_b64,
        "edited": True,
    }


def _key_line(key_b64: str, pref: Optional[int]) -> dict:
    params = [("TYPE", "public"), ("ALG", "ed25519")]
    if pref is not None:
        params.append(("PREF", str(pref)))
    params.append(("ENCODING", "b"))
    return {"name": "KEY", "params": params, "raw_value": key_b64, "edited": True}


def _check(profile: Profile) -> None:
    if profile.errors:
        raise ValueError("The vCard has malformed lines: " + "; ".join(profile.errors))


def revoke_key(
    text: str, key_b64: str, reason: str, when: Optional[datetime] = None
) -> str:
    """Add a REVKEY for a key listed in the vCard and return the new vCard text.

    If the revoked key was the preferred one, it loses its PREF parameter, so the
    vCard has no preferred key until a new one is added.
    """
    if reason not in REVOCATION_REASONS:
        raise ValueError(
            f"Unknown reason '{reason}' (expected one of "
            f"{', '.join(sorted(REVOCATION_REASONS))})"
        )
    load_public_key_b64_der(key_b64)
    when = when or datetime.now(timezone.utc)
    profile = parse_vcard(text)
    _check(profile)

    if not any(k.key_b64 == key_b64 for k in profile.keys):
        raise ValueError("The key is not listed as KEY in the vCard")
    if any(r.key_b64 == key_b64 for r in profile.revocations):
        raise ValueError("The key is already revoked in the vCard")

    lines = []
    alg = "ed25519"
    for line in _lines(profile):
        if line["name"] == "KEY" and line["raw_value"].strip() == key_b64:
            alg = next((v for n, v in line["params"] if n == "ALG" and v), alg)
            line = _without_pref(line)
        lines.append(line)
    lines.append(_revkey_line(key_b64, reason, when, alg))
    return _render(profile, lines)


def rotate_key(
    text: str,
    new_key_b64: str,
    old_key_b64: Optional[str] = None,
    reason: str = "rotated",
    when: Optional[datetime] = None,
) -> str:
    """Make a new key the preferred one and revoke the old one.

    The old key defaults to the current preferred key (PREF=1).
    """
    if reason not in ("rotated", "superseded"):
        raise ValueError("A rotation reason must be 'rotated' or 'superseded'")
    load_public_key_b64_der(new_key_b64)
    when = when or datetime.now(timezone.utc)
    profile = parse_vcard(text)
    _check(profile)

    if any(k.key_b64 == new_key_b64 for k in profile.keys):
        raise ValueError("The new key is already listed as KEY")
    if old_key_b64 is None:
        preferred = [k for k in profile.keys if k.pref == 1]
        if len(preferred) != 1:
            raise ValueError(
                "Cannot tell which key to rotate: the vCard must have exactly one "
                "KEY with PREF=1 (or pass the old key explicitly). If every key is "
                "revoked, use `dsipy key add` to add a new one"
            )
        old_key_b64 = preferred[0].key_b64
    if not any(k.key_b64 == old_key_b64 for k in profile.keys):
        raise ValueError("The old key is not listed as KEY in the vCard")
    if any(r.key_b64 == old_key_b64 for r in profile.revocations):
        raise ValueError("The old key is already revoked in the vCard")

    lines = []
    last_key_index = -1
    for line in _lines(profile):
        if line["name"] == "KEY":
            line = _without_pref(line)
            last_key_index = len(lines)
        lines.append(line)

    insert_at = last_key_index + 1 if last_key_index >= 0 else len(lines)
    lines.insert(insert_at, _key_line(new_key_b64, pref=1))
    lines.append(_revkey_line(old_key_b64, reason, when))
    return _render(profile, lines)


def add_key(text: str, new_key_b64: str, preferred: bool = True) -> str:
    """Add a KEY to the vCard without revoking anything.

    Works when the vCard has no key or when every key is revoked. With
    ``preferred`` the new key gets PREF=1 and the other keys lose PREF.
    """
    load_public_key_b64_der(new_key_b64)
    profile = parse_vcard(text)
    _check(profile)

    if any(k.key_b64 == new_key_b64 for k in profile.keys):
        raise ValueError("The key is already listed as KEY")
    if any(r.key_b64 == new_key_b64 for r in profile.revocations):
        raise ValueError("The key is revoked in the vCard and cannot be added again")

    lines = []
    last_key_index = -1
    for line in _lines(profile):
        if line["name"] == "KEY":
            if preferred:
                line = _without_pref(line)
            last_key_index = len(lines)
        lines.append(line)

    insert_at = last_key_index + 1 if last_key_index >= 0 else len(lines)
    lines.insert(insert_at, _key_line(new_key_b64, pref=1 if preferred else None))
    return _render(profile, lines)
